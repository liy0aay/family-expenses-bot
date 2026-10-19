package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/liy0aay/family-expenses-bot/internal/clock"
	"github.com/liy0aay/family-expenses-bot/internal/config"
	"github.com/liy0aay/family-expenses-bot/internal/domain"
)

const llmHTTPTimeout = 60 * time.Second

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name string `json:"name"`
	// raw JSON, echoed back into history unchanged
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Client struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	http        *http.Client
	clk         clock.Clock
}

func NewClient(cfg config.LLM, clk clock.Clock) *Client {
	return &Client{
		baseURL:     strings.TrimSuffix(cfg.BaseURL, "/"),
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		http:        &http.Client{Timeout: llmHTTPTimeout},
		clk:         clk,
	}
}

func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	start := c.clk.Now()

	body, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.temperature,
	})
	if err != nil {
		return Message{}, fmt.Errorf("%w: encode chat request: %w", domain.ErrInternal, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("%w: build chat request: %w", domain.ErrInternal, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("%w: chat completion: %w", domain.ErrInternal, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("llm response body close", "error", cerr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("%w: read chat response: %w", domain.ErrInternal, err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("%w: chat completion status %d: %s", domain.ErrInternal, resp.StatusCode, truncateBody(respBody))
	}

	var parsed chatResponse
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return Message{}, fmt.Errorf("%w: decode chat response: %w", domain.ErrInternal, err)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, fmt.Errorf("%w: chat completion returned no choices", domain.ErrInternal)
	}

	out := Message{Role: RoleAssistant}
	choice := parsed.Choices[0].Message
	if choice.Content != nil {
		out.Content = *choice.Content
	}
	for _, tc := range choice.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	slog.Info("llm chat completion",
		"model", c.model,
		"duration_ms", c.clk.Now().Sub(start).Milliseconds(),
		"tool_calls", len(out.ToolCalls),
	)
	return out, nil
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Role      string  `json:"role"`
			Content   *string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// UseNumber: amounts must stay json.Number, never float64.
func ParseToolArguments(raw string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return nil, fmt.Errorf("%w: tool arguments: %w", domain.ErrValidation, err)
	}
	return args, nil
}

func truncateBody(b []byte) string {
	const max = 512
	runes := []rune(string(b))
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}
