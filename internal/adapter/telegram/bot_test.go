package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/liy0aay/family-expenses-bot/internal/service"
)

var errFakeUpsert = errors.New("fake upsert failure")

type fakeAgent struct {
	mu      sync.Mutex
	queries []int64
	texts   []string
	ctxs    []context.Context
	reply   string
}

func (f *fakeAgent) Handle(ctx context.Context, chatID int64, text string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, chatID)
	f.texts = append(f.texts, text)
	f.ctxs = append(f.ctxs, ctx)
	return f.reply
}

type fakeUsers struct {
	mu       sync.Mutex
	upserts  []int64
	failFrom int64 // when non-zero, upserts of this id fail
}

func (f *fakeUsers) UpsertUser(_ context.Context, id int64, _, _ string) (service.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts = append(f.upserts, id)
	if id == f.failFrom {
		return service.User{}, errFakeUpsert
	}
	return service.User{ID: id, FirstName: "Алиса"}, nil
}

type sentMessage struct {
	ChatID int64
	Text   string
}

type fakeTelegram struct {
	mu     sync.Mutex
	sent   []sentMessage
	token  string
	result []any
}

func (f *fakeTelegram) handle(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/bot"+f.token+"/") {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/sendMessage"):
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.sent = append(f.sent, sentMessage{ChatID: body.ChatID, Text: body.Text})
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case strings.HasSuffix(r.URL.Path, "/getUpdates"):
		_ = json.NewEncoder(w).Encode(f.updates())
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeTelegram) updates() map[string]any {
	return map[string]any{"ok": true, "result": f.result}
}

func newBotEnv(t *testing.T, tg *fakeTelegram, ag *fakeAgent, users *fakeUsers) *Bot {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(tg.handle))
	t.Cleanup(ts.Close)

	b := New(tg.token, ag, users)
	b.api = ts.URL
	t.Cleanup(func() { b.http.CloseIdleConnections() })
	return b
}

func update(id int64, chatID int64, text string) map[string]any {
	return map[string]any{
		"update_id": id,
		"message": map[string]any{
			"text": text,
			"chat": map[string]any{"id": chatID},
			"from": map[string]any{"id": chatID, "username": "alice", "first_name": "Алиса"},
		},
	}
}

func TestUpdateReachesAgentAndAnswerIsSent(t *testing.T) {
	tg := &fakeTelegram{token: "T", result: []any{update(7, 101, "кофе 350")}}
	ag := &fakeAgent{reply: "Добавил: кофе, 350 ₽, сегодня."}
	users := &fakeUsers{}
	b := newBotEnv(t, tg, ag, users)

	offset, err := b.poll(context.Background(), 0)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if offset != 8 {
		t.Fatalf("offset = %d, want 8", offset)
	}
	if len(ag.texts) != 1 || ag.texts[0] != "кофе 350" || ag.queries[0] != 101 {
		t.Fatalf("agent got %+v", ag.texts)
	}
	if ag.ctxs[0] == context.Background() {
		t.Fatal("agent must receive the scoped ctx, not the raw poll ctx")
	}
	if len(users.upserts) != 1 || users.upserts[0] != 101 {
		t.Fatalf("upserts = %v, want [101]", users.upserts)
	}
	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.sent) != 1 || tg.sent[0].ChatID != 101 || tg.sent[0].Text != "Добавил: кофе, 350 ₽, сегодня." {
		t.Fatalf("sent = %+v", tg.sent)
	}
}

func TestForeignExpenseAnswerHidesForeignData(t *testing.T) {
	tg := &fakeTelegram{token: "T", result: []any{update(3, 102, "удали трату 55")}}
	// real not_found wording from errors.md
	ag := &fakeAgent{reply: "Такой траты нет."}
	users := &fakeUsers{}
	b := newBotEnv(t, tg, ag, users)

	if _, err := b.poll(context.Background(), 0); err != nil {
		t.Fatalf("poll: %v", err)
	}
	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.sent) != 1 {
		t.Fatalf("sent = %+v", tg.sent)
	}
	if !strings.Contains(tg.sent[0].Text, "Такой траты нет") {
		t.Fatalf("text = %q, want not_found wording", tg.sent[0].Text)
	}
	for _, leaked := range []string{"350", "кофе", "Алиса"} {
		if strings.Contains(tg.sent[0].Text, leaked) {
			t.Fatalf("foreign details leaked into %q: %q", tg.sent[0].Text, leaked)
		}
	}
}

func TestUpsertFailureSendsInternalText(t *testing.T) {
	tg := &fakeTelegram{token: "T", result: []any{update(1, 101, "кофе 350")}}
	ag := &fakeAgent{reply: "must not be called"}
	users := &fakeUsers{failFrom: 101}
	b := newBotEnv(t, tg, ag, users)

	if _, err := b.poll(context.Background(), 0); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(ag.texts) != 0 {
		t.Fatal("agent must not run when the user upsert fails")
	}
	tg.mu.Lock()
	defer tg.mu.Unlock()
	if len(tg.sent) != 1 || tg.sent[0].Text != internalText {
		t.Fatalf("sent = %+v, want the internal text", tg.sent)
	}
}

func TestNonMessageUpdatesAreSkipped(t *testing.T) {
	tg := &fakeTelegram{token: "T", result: []any{
		map[string]any{"update_id": 1},
		map[string]any{"update_id": 2, "message": map[string]any{"chat": map[string]any{"id": 101}}},
		update(3, 101, "привет"),
	}}
	ag := &fakeAgent{reply: "ok"}
	users := &fakeUsers{}
	b := newBotEnv(t, tg, ag, users)

	offset, err := b.poll(context.Background(), 0)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if offset != 4 {
		t.Fatalf("offset = %d, want 4", offset)
	}
	if len(ag.texts) != 1 || ag.texts[0] != "привет" {
		t.Fatalf("agent texts = %v, want only the text update", ag.texts)
	}
}
