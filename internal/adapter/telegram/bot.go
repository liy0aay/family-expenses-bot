package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/liy0aay/family-expenses-bot/internal/service"
)

const (
	defaultAPIBase    = "https://api.telegram.org"
	longPollSeconds   = 50
	httpClientTimeout = 65 * time.Second
	pollRetryDelay    = 3 * time.Second
)

type Agent interface {
	Handle(ctx context.Context, chatID int64, text string) string
}

type Users interface {
	UpsertUser(ctx context.Context, id int64, username, firstName string) (service.User, error)
}

const internalText = "Внутренняя ошибка, попробуй позже."

type Bot struct {
	token string
	api   string
	agent Agent
	users Users
	http  *http.Client
}

func New(token string, agent Agent, users Users) *Bot {
	return &Bot{
		token: token,
		api:   defaultAPIBase,
		agent: agent,
		users: users,
		http:  &http.Client{Timeout: httpClientTimeout},
	}
}

func (b *Bot) Start(ctx context.Context) {
	go b.pollLoop(ctx)
}

func (b *Bot) pollLoop(ctx context.Context) {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		next, err := b.pollOnce(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram getUpdates", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollRetryDelay):
			}
			continue
		}
		if next > 0 {
			offset = next
		}
	}
}

// pollOnce contains a panic inside one cycle so the loop retries instead of dying.
func (b *Bot) pollOnce(ctx context.Context, offset int64) (next int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("telegram polling panic", "panic", r, "stack", string(debug.Stack()))
			next, err = 0, fmt.Errorf("panic: %v", r)
		}
	}()
	return b.poll(ctx, offset)
}

func (b *Bot) poll(ctx context.Context, offset int64) (int64, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=%d&offset=%d", b.api, b.token, longPollSeconds, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build getUpdates request: %w", err)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("getUpdates: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("telegram getUpdates body close", "error", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("getUpdates status %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		Result []struct {
			UpdateID int64    `json:"update_id"`
			Message  *message `json:"message"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode getUpdates: %w", err)
	}

	next := offset
	for _, up := range payload.Result {
		if up.UpdateID >= next {
			next = up.UpdateID + 1
		}
		if up.Message == nil || up.Message.Text == "" {
			continue
		}
		b.handleUpdate(ctx, up.UpdateID, *up.Message)
	}
	return next, nil
}

type message struct {
	Text string `json:"text"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	} `json:"from"`
}

func (b *Bot) handleUpdate(ctx context.Context, updateID int64, msg message) {
	chatID := msg.Chat.ID
	// agent tool calls resolve the user from ctx; the scope must wrap the whole turn
	ctx = service.WithUserScope(ctx, chatID)
	if _, err := b.users.UpsertUser(ctx, chatID, msg.From.Username, msg.From.FirstName); err != nil {
		slog.Error("telegram upsert user", "chat_id", chatID, "update_id", updateID, "error", err)
		b.send(ctx, chatID, internalText)
		return
	}
	b.send(ctx, chatID, b.agent.Handle(ctx, chatID, msg.Text))
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) {
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text})
	if err != nil {
		slog.Error("telegram sendMessage marshal", "chat_id", chatID, "error", err)
		return
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", b.api, b.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("telegram sendMessage request", "chat_id", chatID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		slog.Warn("telegram sendMessage", "chat_id", chatID, "error", err)
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("telegram sendMessage body close", "error", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Warn("telegram sendMessage status", "chat_id", chatID, "status", resp.StatusCode, "body", string(snippet))
	}
}
