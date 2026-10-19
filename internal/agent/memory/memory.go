package memory

import (
	"sync"
	"time"

	"github.com/liy0aay/family-expenses-bot/internal/clock"
)

type Message struct {
	Role    string
	Content string
}

type Memory struct {
	mu     sync.Mutex
	window int
	ttl    time.Duration
	clk    clock.Clock
	chats  map[int64]*chat
}

type chat struct {
	messages   []Message
	expenseIDs []int64
	lastActive time.Time
}

func New(window int, ttl time.Duration, clk clock.Clock) *Memory {
	return &Memory{
		window: window,
		ttl:    ttl,
		clk:    clk,
		chats:  make(map[int64]*chat),
	}
}

func (m *Memory) Append(chatID int64, msg Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.live(chatID)
	c.messages = append(c.messages, msg)
	if len(c.messages) > m.window {
		c.messages = c.messages[len(c.messages)-m.window:]
	}
}

func (m *Memory) History(chatID int64) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.current(chatID)
	if !ok {
		return nil
	}
	out := make([]Message, len(c.messages))
	copy(out, c.messages)
	return out
}

func (m *Memory) RememberExpense(chatID, expenseID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.live(chatID)
	c.expenseIDs = append(c.expenseIDs, expenseID)
	if len(c.expenseIDs) > m.window {
		c.expenseIDs = c.expenseIDs[len(c.expenseIDs)-m.window:]
	}
}

func (m *Memory) LastExpense(chatID int64) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.current(chatID)
	if !ok || len(c.expenseIDs) == 0 {
		return 0, false
	}
	return c.expenseIDs[len(c.expenseIDs)-1], true
}

func (m *Memory) RecentExpenses(chatID int64, n int) []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.current(chatID)
	if !ok || len(c.expenseIDs) == 0 {
		return nil
	}
	last := len(c.expenseIDs)
	first := last - n
	if first < 0 {
		first = 0
	}
	out := make([]int64, 0, last-first)
	for i := last - 1; i >= first; i-- {
		out = append(out, c.expenseIDs[i])
	}
	return out
}

// only writes refresh lastActive; reads never extend the ttl.
func (m *Memory) live(chatID int64) *chat {
	c, ok := m.current(chatID)
	if !ok {
		c = &chat{}
		m.chats[chatID] = c
	}
	c.lastActive = m.clk.Now()
	return c
}

func (m *Memory) current(chatID int64) (*chat, bool) {
	c, ok := m.chats[chatID]
	if !ok {
		return nil, false
	}
	if m.clk.Now().Sub(c.lastActive) > m.ttl {
		delete(m.chats, chatID)
		return nil, false
	}
	return c, true
}
