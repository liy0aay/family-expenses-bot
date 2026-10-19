package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/liy0aay/family-expenses-bot/internal/agent/memory"
	"github.com/liy0aay/family-expenses-bot/internal/clock"
)

//go:embed prompt.md
var systemPrompt string

const (
	maxToolIterations = 4

	llmFailText = "Временная проблема с ИИ, попробуй чуть позже."
	giveUpText  = "Не получилось обработать запрос. Попробуй упростить его или разбить на несколько сообщений."
	recentIDs   = 3
)

type Dispatcher interface {
	Definitions() []Tool
	Dispatch(ctx context.Context, name, arguments string) (any, error)
}

type Agent struct {
	llm    *Client
	tools  Dispatcher
	memory *memory.Memory
	clk    clock.Clock
}

func New(llm *Client, tl Dispatcher, mem *memory.Memory, clk clock.Clock) *Agent {
	return &Agent{llm: llm, tools: tl, memory: mem, clk: clk}
}

// user identity travels only in ctx; the bot sets the scope before calling.
func (a *Agent) Handle(ctx context.Context, chatID int64, text string) string {
	a.memory.Append(chatID, memory.Message{Role: "user", Content: text})

	history := a.memory.History(chatID)
	messages := make([]Message, 0, len(history)+1)
	messages = append(messages, Message{Role: RoleSystem, Content: a.systemMessage(chatID)})
	for _, m := range history {
		// tool results stay in memory for window accounting, but a replayed tool
		// message has no tool_call_id anchor, which strict providers reject
		if m.Role == "tool" {
			continue
		}
		messages = append(messages, Message{Role: Role(m.Role), Content: m.Content})
	}
	definitions := a.tools.Definitions()

	for range maxToolIterations {
		resp, err := a.llm.Chat(ctx, messages, definitions)
		if err != nil {
			slog.Error("agent llm chat", "chat_id", chatID, "error", err)
			return a.finish(chatID, llmFailText)
		}
		if len(resp.ToolCalls) == 0 {
			// an empty content would be rejected by the Telegram API as an empty message;
			// an empty model reply is the same transient class as a transport failure —
			// Warn instead of Error: the request reached the model, the model did not answer
			if strings.TrimSpace(resp.Content) == "" {
				slog.Warn("agent empty llm reply", "chat_id", chatID)
				return a.finish(chatID, llmFailText)
			}
			return a.finish(chatID, resp.Content)
		}
		messages = append(messages, resp)
		for _, tc := range resp.ToolCalls {
			result := a.execTool(ctx, chatID, tc)
			messages = append(messages, Message{Role: RoleTool, ToolCallID: tc.ID, Content: result})
			a.memory.Append(chatID, memory.Message{Role: "tool", Content: result})
		}
	}
	return a.finish(chatID, giveUpText)
}

func (a *Agent) finish(chatID int64, text string) string {
	a.memory.Append(chatID, memory.Message{Role: "assistant", Content: text})
	return text
}

func (a *Agent) systemMessage(chatID int64) string {
	dialogCtx := ""
	if id, ok := a.memory.LastExpense(chatID); ok {
		dialogCtx += fmt.Sprintf("Последняя упомянутая трата: id %d.", id)
	}
	if ids := a.memory.RecentExpenses(chatID, recentIDs); len(ids) > 0 {
		dialogCtx += fmt.Sprintf(" Недавно упомянутые id (свежие раньше): %v.", ids)
	}
	return fmt.Sprintf(systemPrompt, a.clk.Now().Format("2006-01-02"), dialogCtx)
}

// failures reach the LLM as {error:{code,message}} (contract rule 5).
func (a *Agent) execTool(ctx context.Context, chatID int64, tc ToolCall) string {
	out, err := a.tools.Dispatch(ctx, tc.Function.Name, tc.Function.Arguments)
	var payload any
	if err != nil {
		payload = errorPayload(err)
	} else {
		payload = out
		a.rememberIDs(chatID, tc.Function.Name, out)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		slog.Error("agent tool result marshal", "tool", tc.Function.Name, "error", err)
		return `{"error":{"code":"internal","message":"tool result encoding failed"}}`
	}
	return string(b)
}

func errorPayload(err error) map[string]any {
	code, message := "internal", err.Error()
	var te *ToolError
	if errors.As(err, &te) {
		code, message = te.Code, te.Message
	}
	return map[string]any{"error": map[string]any{"code": code, "message": message}}
}

func (a *Agent) rememberIDs(chatID int64, tool string, out any) {
	m, ok := out.(map[string]any)
	if !ok {
		return
	}
	if found, ok := m["expenses"].([]map[string]any); ok {
		// find returns newest first; remember reversed so the memory tail keeps the freshest id
		for i := len(found) - 1; i >= 0; i-- {
			if id, ok := found[i]["id"].(int64); ok {
				a.memory.RememberExpense(chatID, id)
			}
		}
	}
	if id, ok := m["id"].(int64); ok {
		a.memory.RememberExpense(chatID, id)
	}
}
