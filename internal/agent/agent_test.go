package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/agent"
	"github.com/liy0aay/family-expenses-bot/internal/agent/memory"
	"github.com/liy0aay/family-expenses-bot/internal/agent/tools"
	"github.com/liy0aay/family-expenses-bot/internal/clock"
	"github.com/liy0aay/family-expenses-bot/internal/config"
	"github.com/liy0aay/family-expenses-bot/internal/service"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

const (
	aliceID = 101
	chatID  = 1000
)

// capturedRequest mirrors the wire request the agent sends to the LLM.
type capturedRequest struct {
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
}

func (c capturedRequest) systemMessage() string {
	for _, m := range c.Messages {
		if m.Role == "system" {
			return m.Content
		}
	}
	return ""
}

func (c capturedRequest) toolMessages() []string {
	var out []string
	for _, m := range c.Messages {
		if m.Role == "tool" {
			out = append(out, m.Content)
		}
	}
	return out
}

type fakeLLM struct {
	mu        sync.Mutex
	calls     int
	responses []map[string]any
	requests  []capturedRequest
}

func (f *fakeLLM) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req capturedRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, req)
		resp := f.responses[min(f.calls, len(f.responses)-1)]
		f.calls++
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (f *fakeLLM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func contentReply(text string) map[string]any {
	return map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "content": text},
		}},
	}
}

func toolReply(calls ...map[string]any) map[string]any {
	return map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "tool_calls": calls},
		}},
	}
}

func toolCall(id, name, argsJSON string) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     "function",
		"function": map[string]any{"name": name, "arguments": argsJSON},
	}
}

func newEnvPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, expenses RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func newAgentEnv(t *testing.T, llm *httptest.Server) (*agent.Agent, *pgxpool.Pool, *memory.Memory) {
	t.Helper()

	pool := newEnvPool(t)
	seedUser(t, pool, aliceID)

	clk := &clock.Fixed{T: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	mem := memory.New(8, 30*time.Minute, clk)
	expenses := service.NewExpenseService(pool)
	tl := tools.New(expenses, clk)
	client := agent.NewClient(config.LLM{BaseURL: llm.URL, APIKey: "test", Model: "fake"}, clk)
	return agent.New(client, tl, mem, clk), pool, mem
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id int64) {
	t.Helper()

	users := service.NewUserService(pool)
	_, err := users.UpsertUser(
		service.WithFamilyHeadScope(context.Background(), nil),
		id, fmt.Sprintf("user%d", id), fmt.Sprintf("Name%d", id),
	)
	if err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

func mustCreate(t *testing.T, pool *pgxpool.Pool, in service.CreateInput) service.Expense {
	t.Helper()

	expenses := service.NewExpenseService(pool)
	e, err := expenses.Create(service.WithUserScope(context.Background(), aliceID), in)
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	return e
}

func expenseCount(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var n int64
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM expenses").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func scoped(ctx context.Context) context.Context {
	return service.WithUserScope(ctx, aliceID)
}

func TestGreetingRunsNoTools(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{contentReply("Привет! Я помогаю учитывать расходы.")}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, _ := newAgentEnv(t, llm)

	reply := ag.Handle(scoped(context.Background()), chatID, "привет")

	if reply != "Привет! Я помогаю учитывать расходы." {
		t.Fatalf("reply = %q", reply)
	}
	if fake.callCount() != 1 {
		t.Fatalf("llm calls = %d, want 1", fake.callCount())
	}
	if n := expenseCount(t, pool); n != 0 {
		t.Fatalf("greeting must not touch expenses, got %d rows", n)
	}
}

func TestThanksRunsNoTools(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{contentReply("Всегда рад помочь!")}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, _ := newAgentEnv(t, llm)

	ag.Handle(scoped(context.Background()), chatID, "спасибо")

	if n := expenseCount(t, pool); n != 0 {
		t.Fatalf("thanks must not touch expenses, got %d rows", n)
	}
}

func TestStopsAfterFourIterationsWithPoliteAnswer(t *testing.T) {
	add := toolCall("c1", "add_expense", `{"amount_rub":350,"category":"food","spent_at":"2026-10-19"}`)
	fake := &fakeLLM{responses: []map[string]any{toolReply(add), toolReply(add), toolReply(add), toolReply(add)}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, mem := newAgentEnv(t, llm)

	reply := ag.Handle(scoped(context.Background()), chatID, "кофе 350")

	if reply != "Не получилось обработать запрос. Попробуй упростить его или разбить на несколько сообщений." {
		t.Fatalf("reply = %q, want the give-up fallback", reply)
	}
	if fake.callCount() != 4 {
		t.Fatalf("llm calls = %d, want 4", fake.callCount())
	}
	if n := expenseCount(t, pool); n != 4 {
		t.Fatalf("each iteration must run its tools, got %d rows", n)
	}
	history := mem.History(chatID)
	if last := history[len(history)-1]; last.Role != "assistant" || last.Content != reply {
		t.Fatalf("polite answer must be stored as the assistant turn, got %+v", last)
	}
}

func TestHistoryReplaysFullToolTrace(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{
		toolReply(toolCall("c1", "add_expense", `{"amount_rub":350,"category":"food","spent_at":"2026-10-19"}`)),
		contentReply("Записал: кофе — 350 ₽ (еда), сегодня."),
		contentReply("Готово."),
	}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, _, _ := newAgentEnv(t, llm)

	ag.Handle(scoped(context.Background()), chatID, "кофе 350")
	ag.Handle(scoped(context.Background()), chatID, "что записал?")

	msgs := fake.requests[2].Messages
	trace := -1
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].Function.Name == "add_expense" {
			trace = i
			break
		}
	}
	if trace == -1 {
		t.Fatal("replayed history must contain the assistant turn with its tool_calls")
	}
	if msgs[trace].ToolCalls[0].ID != "c1" {
		t.Fatalf("tool call id = %q, want c1", msgs[trace].ToolCalls[0].ID)
	}
	if msgs[trace+1].Role != "tool" || msgs[trace+1].ToolCallID != "c1" {
		t.Fatalf("message after tool_calls = %s (tool_call_id %q), want a tool result anchored to c1", msgs[trace+1].Role, msgs[trace+1].ToolCallID)
	}
	if msgs[trace+2].Role != "assistant" || msgs[trace+2].Content != "Записал: кофе — 350 ₽ (еда), сегодня." {
		t.Fatalf("final assistant reply must follow the tool result, got %s %q", msgs[trace+2].Role, msgs[trace+2].Content)
	}
	if msgs[trace+3].Role != "user" || msgs[trace+3].Content != "что записал?" {
		t.Fatalf("the new user message must come last, got %s %q", msgs[trace+3].Role, msgs[trace+3].Content)
	}
}

func TestInvalidAmountLeavesNoExpense(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{
		toolReply(toolCall("c1", "add_expense", `{"amount_rub":350.505,"category":"food","spent_at":"2026-10-19"}`)),
		contentReply("Не понял сумму. Пример: 350 или 350.50."),
	}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, _ := newAgentEnv(t, llm)

	reply := ag.Handle(scoped(context.Background()), chatID, "кофе 350.505")

	if !strings.Contains(reply, "сумму") {
		t.Fatalf("reply = %q, want a friendly amount explanation", reply)
	}
	if n := expenseCount(t, pool); n != 0 {
		t.Fatalf("invalid amount must not create an expense, got %d rows", n)
	}
	// the tool failure reaches the LLM as {error:{code,message}}
	toolMsgs := fake.requests[1].toolMessages()
	if len(toolMsgs) != 1 || !strings.Contains(toolMsgs[0], `"code":"invalid_amount"`) {
		t.Fatalf("tool result = %v, want invalid_amount error payload", toolMsgs)
	}
}

func TestLastExpenseResolvedFromMemory(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{contentReply("Готово.")}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, mem := newAgentEnv(t, llm)
	e := mustCreate(t, pool, service.CreateInput{AmountMinor: 35000, Category: "food", SpentAt: "2026-10-19", Comment: "кофе"})
	mem.RememberExpense(chatID, e.ID)

	ag.Handle(scoped(context.Background()), chatID, "последнее было не 350, а 450")

	system := fake.requests[0].systemMessage()
	if !strings.Contains(system, fmt.Sprintf("id %d", e.ID)) {
		t.Fatalf("system prompt must carry the remembered id %d, got %q", e.ID, system)
	}
}

func TestLastExpenseFallsBackToFind(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{
		toolReply(toolCall("c1", "find_expenses", `{"limit":1}`)),
		contentReply("Готово."),
	}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, mem := newAgentEnv(t, llm)
	e := mustCreate(t, pool, service.CreateInput{AmountMinor: 35000, Category: "food", SpentAt: "2026-10-19", Comment: "кофе"})

	ag.Handle(scoped(context.Background()), chatID, "последнее было не 350, а 450")

	if system := fake.requests[0].systemMessage(); strings.Contains(system, "Последняя упомянутая") {
		t.Fatalf("empty memory must not claim a last expense, got %q", system)
	}
	if id, ok := mem.LastExpense(chatID); !ok || id != e.ID {
		t.Fatalf("fallback find must remember the found id, got (%d, %v), want %d", id, ok, e.ID)
	}
}

func TestDeleteWithSeveralMatchesShowsCandidates(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{
		toolReply(toolCall("c1", "find_expenses", `{"text":"такси"}`)),
		contentReply("Нашёл две поездки: id 2 и id 1. Какую удалить?"),
	}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, pool, mem := newAgentEnv(t, llm)
	first := mustCreate(t, pool, service.CreateInput{AmountMinor: 50000, Category: "transport", SpentAt: "2026-10-18", Comment: "такси в центр"})
	second := mustCreate(t, pool, service.CreateInput{AmountMinor: 70000, Category: "transport", SpentAt: "2026-10-19", Comment: "такси в аэропорт"})

	reply := ag.Handle(scoped(context.Background()), chatID, "удали такси")

	if got := strings.Count(reply, "?"); got != 1 {
		t.Fatalf("reply = %q, want exactly one clarifying question", reply)
	}
	toolMsgs := fake.requests[1].toolMessages()
	joined := strings.Join(toolMsgs, " ")
	for _, id := range []int64{first.ID, second.ID} {
		if !strings.Contains(joined, fmt.Sprintf(`"id":%d`, id)) {
			t.Fatalf("candidates must reach the LLM, missing id %d in %q", id, joined)
		}
	}
	recent := mem.RecentExpenses(chatID, 5)
	if len(recent) != 2 || recent[0] != second.ID || recent[1] != first.ID {
		t.Fatalf("memory must hold both candidates newest first, got %v", recent)
	}
}

func TestEmptyContentFallsBackToLlmFailText(t *testing.T) {
	fake := &fakeLLM{responses: []map[string]any{contentReply("   ")}}
	llm := httptest.NewServer(fake.handler())
	defer llm.Close()
	ag, _, mem := newAgentEnv(t, llm)

	reply := ag.Handle(scoped(context.Background()), chatID, "привет")

	if reply != "Временная проблема с ИИ, попробуй чуть позже." {
		t.Fatalf("reply = %q, want the llm-fail fallback", reply)
	}
	if last := mem.History(chatID); last[len(last)-1].Content != reply {
		t.Fatal("fallback text must be stored as the assistant turn")
	}
}

func TestLLMFailureReturnsFriendlyText(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer llm.Close()
	ag, _, mem := newAgentEnv(t, llm)

	reply := ag.Handle(scoped(context.Background()), chatID, "кофе 350")

	if !strings.Contains(reply, "попробуй чуть позже") {
		t.Fatalf("reply = %q, want the friendly llm-failure text", reply)
	}
	if last := mem.History(chatID); len(last) == 0 || last[len(last)-1].Content != reply {
		t.Fatal("failure text must be stored as the assistant turn")
	}
}
