//go:build eval

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/liy0aay/family-expenses-bot/internal/agent"
	"github.com/liy0aay/family-expenses-bot/internal/agent/memory"
	"github.com/liy0aay/family-expenses-bot/internal/agent/tools"
	"github.com/liy0aay/family-expenses-bot/internal/clock"
	"github.com/liy0aay/family-expenses-bot/internal/config"
	"github.com/liy0aay/family-expenses-bot/internal/service"
)

const (
	referenceDate = "2026-10-15"
	passThreshold = 0.9
	maxRetries    = 2
	retryPause    = 2 * time.Second

	envOnly = "EVALS_ONLY"
)

//go:embed dataset.yaml
var datasetRaw []byte

type dataset struct {
	Cases []evalCase `yaml:"cases"`
}

type evalCase struct {
	ID     string       `yaml:"id"`
	Chat   int64        `yaml:"chat"`
	Input  string       `yaml:"input"`
	Expect expectations `yaml:"expect"`
}

type expectations struct {
	ToolsMin    *int     `yaml:"tools_min"`
	ToolsMax    *int     `yaml:"tools_max"`
	CountDelta  *int     `yaml:"count_delta"`
	ContainsAny []string `yaml:"contains_any"`
}

type recordedCall struct {
	name      string
	arguments string
}

type loggingDispatcher struct {
	next  agent.Dispatcher
	calls []recordedCall
}

func (d *loggingDispatcher) Definitions() []agent.Tool { return d.next.Definitions() }

func (d *loggingDispatcher) Dispatch(ctx context.Context, name, arguments string) (any, error) {
	d.calls = append(d.calls, recordedCall{name: name, arguments: arguments})
	slog.Info("eval tool call", "tool", name, "args", arguments)
	return d.next.Dispatch(ctx, name, arguments)
}

type caseResult struct {
	caseID   string
	ok       bool
	tools    []string
	delta    int
	duration time.Duration
	reply    string
	reason   string
}

func main() {
	if err := run(); err != nil {
		slog.Error("evals failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	loadDotEnv(".env")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if cfg.LLM.BaseURL == "" || cfg.LLM.APIKey == "" || cfg.LLM.Model == "" {
		return errors.New("LLM_BASE_URL / LLM_API_KEY / LLM_MODEL are not set: fill .env (see .env.example)")
	}

	dsn := firstNonEmpty(os.Getenv("EVALS_POSTGRES_DSN"), os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		return errors.New("EVALS_POSTGRES_DSN (or TEST_POSTGRES_DSN) is not set: e.g. postgres://postgres:postgres@localhost:55450/expenses?sslmode=disable")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "TRUNCATE users, expenses RESTART IDENTITY"); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	refTime, err := time.Parse("2006-01-02", referenceDate)
	if err != nil {
		return fmt.Errorf("reference date: %w", err)
	}
	clk := &clock.Fixed{T: time.Date(refTime.Year(), refTime.Month(), refTime.Day(), 12, 0, 0, 0, time.UTC)}

	var ds dataset
	dec := yaml.NewDecoder(strings.NewReader(string(datasetRaw)))
	dec.KnownFields(true)
	if err := dec.Decode(&ds); err != nil {
		return fmt.Errorf("parse dataset.yaml: %w", err)
	}
	cases := selectCases(ds.Cases)

	expenses := service.NewExpenseService(pool)
	users := service.NewUserService(pool)
	disp := &loggingDispatcher{next: tools.New(expenses, clk)}
	dialogAgent := agent.New(
		agent.NewClient(cfg.LLM, clk),
		disp,
		memory.New(cfg.DialogWindow, cfg.DialogTTL, clk),
		clk,
	)

	fmt.Printf("== evals: reference_time=%s, cases=%d (dataset total %d) ==\n",
		referenceDate, len(cases), len(ds.Cases))

	results := make([]caseResult, 0, len(cases))
	for _, tc := range cases {
		res := runCase(ctx, pool, users, dialogAgent, disp, tc)
		results = append(results, res)
		printResult(res)
	}

	printSummary(results)
	return finish(results)
}

func runCase(
	ctx context.Context,
	pool *pgxpool.Pool,
	users *service.UserService,
	dialogAgent *agent.Agent,
	disp *loggingDispatcher,
	tc evalCase,
) caseResult {
	var last caseResult
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			time.Sleep(retryPause)
		}
		caseCtx := service.WithUserScope(ctx, tc.Chat)
		if _, err := users.UpsertUser(caseCtx, tc.Chat, "", ""); err != nil {
			return caseResult{caseID: tc.ID, reason: fmt.Sprintf("upsert user: %v", err)}
		}

		maxID := maxExpenseID(ctx, pool, tc.Chat)
		before := countExpenses(ctx, pool, tc.Chat)

		disp.calls = nil
		started := time.Now()
		reply := dialogAgent.Handle(caseCtx, tc.Chat, tc.Input)
		after := countExpenses(ctx, pool, tc.Chat)

		last = evaluate(tc, disp.calls, before, after, reply, time.Since(started))

		if reply == agent.LLMFailText && attempt < maxRetries {
			if _, err := pool.Exec(ctx,
				"DELETE FROM expenses WHERE user_id = $1 AND id > $2", tc.Chat, maxID); err != nil {
				slog.Warn("eval retry cleanup", "case", tc.ID, "err", err)
			}
			slog.Warn("eval retry", "case", tc.ID, "attempt", attempt+1, "reply", reply)
			continue
		}
		return last
	}
}

func evaluate(tc evalCase, calls []recordedCall, before, after int, reply string, duration time.Duration) caseResult {
	res := caseResult{
		caseID:   tc.ID,
		tools:    make([]string, 0, len(calls)),
		delta:    after - before,
		duration: duration,
		reply:    reply,
	}
	for _, c := range calls {
		res.tools = append(res.tools, c.name)
	}

	var reasons []string
	n := len(calls)
	if tc.Expect.ToolsMin != nil && n < *tc.Expect.ToolsMin {
		reasons = append(reasons, fmt.Sprintf("tool calls %d < min %d", n, *tc.Expect.ToolsMin))
	}
	if tc.Expect.ToolsMax != nil && n > *tc.Expect.ToolsMax {
		reasons = append(reasons, fmt.Sprintf("tool calls %d > max %d", n, *tc.Expect.ToolsMax))
	}
	if tc.Expect.CountDelta != nil && res.delta != *tc.Expect.CountDelta {
		reasons = append(reasons, fmt.Sprintf("count_delta %d, want %d", res.delta, *tc.Expect.CountDelta))
	}
	if len(tc.Expect.ContainsAny) > 0 {
		if ok, variants := anySubstring(reply, tc.Expect.ContainsAny); !ok {
			reasons = append(reasons, "reply has none of "+variants)
		}
	}

	res.ok = len(reasons) == 0
	res.reason = strings.Join(reasons, "; ")
	return res
}

func anySubstring(reply string, want []string) (bool, string) {
	fold := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "ё", "е") }
	lower := fold(reply)
	var quoted []string
	for _, s := range want {
		if strings.Contains(lower, fold(s)) {
			return true, ""
		}
		quoted = append(quoted, strconv.Quote(s))
	}
	return false, strings.Join(quoted, ", ")
}

func countExpenses(ctx context.Context, pool *pgxpool.Pool, chatID int64) int {
	var n int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM expenses WHERE user_id = $1", chatID).Scan(&n); err != nil {
		slog.Error("eval count expenses", "chat_id", chatID, "err", err)
		return -1
	}
	return n
}

func maxExpenseID(ctx context.Context, pool *pgxpool.Pool, chatID int64) int64 {
	var id int64
	if err := pool.QueryRow(ctx,
		"SELECT COALESCE(MAX(id), 0) FROM expenses WHERE user_id = $1", chatID).Scan(&id); err != nil {
		slog.Error("eval max expense id", "chat_id", chatID, "err", err)
		return 0
	}
	return id
}

func selectCases(all []evalCase) []evalCase {
	raw := os.Getenv(envOnly)
	if strings.TrimSpace(raw) == "" {
		return all
	}
	want := make(map[string]bool)
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	cases := make([]evalCase, 0, len(want))
	for _, tc := range all {
		if want[tc.ID] {
			cases = append(cases, tc)
		}
	}
	slog.Info("evals subset selected", "env", envOnly, "cases", len(cases))
	return cases
}

func printResult(res caseResult) {
	status := "OK  "
	if !res.ok {
		status = "FAIL"
	}
	line := fmt.Sprintf("[%s] %-26s tools=%v delta=%+d took=%s", status, res.caseID, res.tools, res.delta, res.duration.Round(time.Millisecond))
	if !res.ok {
		line += "\n       reason: " + res.reason + "\n       reply: " + oneLine(res.reply)
	}
	fmt.Println(line)
}

func printSummary(results []caseResult) {
	passed := 0
	for _, r := range results {
		if r.ok {
			passed++
		}
	}
	total := len(results)
	rate := 0.0
	if total > 0 {
		rate = float64(passed) / float64(total)
	}
	fmt.Printf("== passed %d/%d (%.1f%%), threshold %.0f%% ==\n", passed, total, rate*100, passThreshold*100)
}

func finish(results []caseResult) error {
	if len(results) == 0 {
		return errors.New("no cases to run")
	}
	passed := 0
	for _, r := range results {
		if r.ok {
			passed++
		}
	}
	if float64(passed)/float64(len(results)) < passThreshold {
		return fmt.Errorf("pass rate %.1f%% is below the %.0f%% threshold",
			float64(passed)/float64(len(results))*100, passThreshold*100)
	}
	return nil
}

func loadDotEnv(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func oneLine(s string) string {
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err == nil {
		s = b.String()
	}
	s = strings.Join(strings.Fields(s), " ")
	const max = 300
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max]) + "..."
	}
	return s
}
