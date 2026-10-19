package memory

import (
	"testing"
	"time"

	"github.com/liy0aay/family-expenses-bot/internal/clock"
)

func newMemory(t *testing.T, window int, ttl time.Duration) (*Memory, *clock.Fixed) {
	t.Helper()
	clk := &clock.Fixed{T: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)}
	return New(window, ttl, clk), clk
}

func advance(clk *clock.Fixed, d time.Duration) {
	clk.T = clk.T.Add(d)
}

func TestWindowEvictionKeepsLastEight(t *testing.T) {
	m, _ := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	roles := []string{"user", "assistant", "tool", "user", "assistant", "tool", "user", "assistant", "tool", "user", "assistant", "user"}
	for i, role := range roles {
		m.Append(chatID, Message{Role: role, Content: string(rune('a' + i))})
	}

	history := m.History(chatID)
	if len(history) != 8 {
		t.Fatalf("history length = %d, want 8", len(history))
	}
	want := "efghijkl"
	got := make([]byte, 0, len(history))
	for _, msg := range history {
		got = append(got, msg.Content[0])
	}
	if string(got) != want {
		t.Fatalf("history contents = %q, want %q", got, want)
	}
	for i, role := range []string{"assistant", "tool", "user", "assistant", "tool", "user", "assistant", "user"} {
		if history[i].Role != role {
			t.Fatalf("history[%d].role = %q, want %q", i, history[i].Role, role)
		}
	}
}

func TestHistoryReturnsCopy(t *testing.T) {
	m, _ := newMemory(t, 8, 30*time.Minute)
	m.Append(1, Message{Role: "user", Content: "x"})

	history := m.History(1)
	history[0].Content = "mutated"

	if m.History(1)[0].Content != "x" {
		t.Fatal("caller mutation leaked into stored history")
	}
}

func TestTTLExpiryResetsState(t *testing.T) {
	m, clk := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	m.Append(chatID, Message{Role: "user", Content: "coffee 100"})
	m.RememberExpense(chatID, 42)

	advance(clk, 29*time.Minute)
	if got := m.History(chatID); len(got) != 1 {
		t.Fatalf("history before ttl = %d messages, want 1", len(got))
	}

	advance(clk, 2*time.Minute)
	if got := m.History(chatID); len(got) != 0 {
		t.Fatalf("history after ttl = %d messages, want 0", len(got))
	}
	if _, ok := m.LastExpense(chatID); ok {
		t.Fatal("LastExpense survived ttl reset")
	}
	if got := m.RecentExpenses(chatID, 5); len(got) != 0 {
		t.Fatalf("RecentExpenses after ttl = %v, want empty", got)
	}
}

func TestActivityRefreshesTTL(t *testing.T) {
	m, clk := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	m.Append(chatID, Message{Role: "user", Content: "first"})
	advance(clk, 20*time.Minute)
	m.Append(chatID, Message{Role: "assistant", Content: "ok"})
	// reads must not refresh the ttl
	advance(clk, 15*time.Minute)
	m.History(chatID)
	advance(clk, 14*time.Minute)

	if got := m.History(chatID); len(got) != 2 {
		t.Fatalf("history reset before ttl (29m since last write), got %d messages", len(got))
	}
	advance(clk, 2*time.Minute)
	if got := m.History(chatID); len(got) != 0 {
		t.Fatalf("history alive after 31m idle with only reads, want reset, got %d messages", len(got))
	}
}

func TestLastExpenseAfterReset(t *testing.T) {
	m, clk := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	m.RememberExpense(chatID, 42)
	if id, ok := m.LastExpense(chatID); !ok || id != 42 {
		t.Fatalf("LastExpense = (%d, %v), want (42, true)", id, ok)
	}

	advance(clk, 31*time.Minute)
	m.Append(chatID, Message{Role: "user", Content: "delete the last one"})
	if _, ok := m.LastExpense(chatID); ok {
		t.Fatal("LastExpense must be unknown in a fresh session")
	}

	m.RememberExpense(chatID, 43)
	if id, ok := m.LastExpense(chatID); !ok || id != 43 {
		t.Fatalf("LastExpense = (%d, %v), want (43, true)", id, ok)
	}
}

func TestRecentExpensesOrderAndCap(t *testing.T) {
	m, _ := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	for _, id := range []int64{1, 2, 3} {
		m.RememberExpense(chatID, id)
	}

	got := m.RecentExpenses(chatID, 2)
	if len(got) != 2 || got[0] != 3 || got[1] != 2 {
		t.Fatalf("RecentExpenses(_, 2) = %v, want [3 2]", got)
	}
	got = m.RecentExpenses(chatID, 10)
	if len(got) != 3 || got[0] != 3 || got[2] != 1 {
		t.Fatalf("RecentExpenses(_, 10) = %v, want [3 2 1]", got)
	}
}

func TestExpenseIDsEvictedByWindow(t *testing.T) {
	m, _ := newMemory(t, 8, 30*time.Minute)
	chatID := int64(100)

	for id := int64(1); id <= 10; id++ {
		m.RememberExpense(chatID, id)
	}
	if id, ok := m.LastExpense(chatID); !ok || id != 10 {
		t.Fatalf("LastExpense = (%d, %v), want (10, true)", id, ok)
	}
	if got := m.RecentExpenses(chatID, 10); len(got) != 8 || got[7] != 3 {
		t.Fatalf("RecentExpenses = %v, want oldest kept id 3", got)
	}
}
