package service_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/domain"
	"github.com/liy0aay/family-expenses-bot/internal/service"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

const (
	userAlice = 101
	userBob   = 102
)

func newTestPool(t *testing.T) *pgxpool.Pool {
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

func mustCreate(t *testing.T, pool *pgxpool.Pool, scopeUserID int64, in service.CreateInput) service.Expense {
	t.Helper()

	expenses := service.NewExpenseService(pool)
	e, err := expenses.Create(service.WithUserScope(context.Background(), scopeUserID), in)
	if err != nil {
		t.Fatalf("create expense for %d: %v", scopeUserID, err)
	}
	return e
}

func TestListUserScopeSeesOnlyOwn(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)
	seedUser(t, pool, userBob)

	mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 10000, Category: "food", SpentAt: "2026-10-19"})
	mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 20000, Category: "transport", SpentAt: "2026-10-18"})
	mustCreate(t, pool, userBob, service.CreateInput{AmountMinor: 5000, Category: "food", SpentAt: "2026-10-19"})

	expenses := service.NewExpenseService(pool)
	ctx := service.WithUserScope(context.Background(), userAlice)

	own, err := expenses.List(ctx, service.ListInput{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(own) != 2 {
		t.Fatalf("alice must see 2 own expenses, got %d", len(own))
	}
	for _, e := range own {
		if e.UserID != userAlice {
			t.Fatalf("alice saw a foreign expense: user_id=%d", e.UserID)
		}
	}
}

func TestListFamilyHeadScopeSeesAll(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)
	seedUser(t, pool, userBob)

	mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 10000, Category: "food", SpentAt: "2026-10-19"})
	mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 20000, Category: "transport", SpentAt: "2026-10-18"})
	mustCreate(t, pool, userBob, service.CreateInput{AmountMinor: 5000, Category: "food", SpentAt: "2026-10-19"})

	expenses := service.NewExpenseService(pool)
	ctx := service.WithFamilyHeadScope(context.Background(), nil)

	all, err := expenses.List(ctx, service.ListInput{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("head must see all 3 expenses, got %d", len(all))
	}

	filter := int64(userAlice)
	aliceOnly, err := expenses.List(service.WithFamilyHeadScope(context.Background(), &filter), service.ListInput{Limit: 10})
	if err != nil {
		t.Fatalf("list with filter: %v", err)
	}
	if len(aliceOnly) != 2 {
		t.Fatalf("head filter must yield a 2-expense subset, got %d", len(aliceOnly))
	}
	for _, e := range aliceOnly {
		if e.UserID != userAlice {
			t.Fatalf("head filter returned user_id=%d, want %d", e.UserID, userAlice)
		}
	}
}

func TestForeignExpenseIsNotFoundForUserScope(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)
	seedUser(t, pool, userBob)

	alien := mustCreate(t, pool, userBob, service.CreateInput{AmountMinor: 7000, Category: "food", SpentAt: "2026-10-19", Comment: "bob lunch"})

	expenses := service.NewExpenseService(pool)
	alice := service.WithUserScope(context.Background(), userAlice)
	head := service.WithFamilyHeadScope(context.Background(), nil)

	if _, err := expenses.Get(alice, alien.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alice get foreign expense: want not_found, got %v", err)
	}
	got, err := expenses.Get(head, alien.ID)
	if err != nil {
		t.Fatalf("head get foreign expense: %v", err)
	}
	if got.ID != alien.ID || got.Comment != "bob lunch" {
		t.Fatalf("head sees a different expense: %+v", got)
	}

	patchAmount := int64(999)
	if _, err := expenses.Patch(alice, alien.ID, service.PatchInput{AmountMinor: &patchAmount}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alice patch foreign expense: want not_found, got %v", err)
	}
	unchanged, err := expenses.Get(head, alien.ID)
	if err != nil {
		t.Fatalf("head get after rejected patch: %v", err)
	}
	if unchanged.AmountMinor != 7000 {
		t.Fatalf("rejected patch must not change data, got amount_minor=%d", unchanged.AmountMinor)
	}

	if err := expenses.Delete(alice, alien.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("alice delete foreign expense: want not_found, got %v", err)
	}
	if err := expenses.Delete(head, alien.ID); err != nil {
		t.Fatalf("head delete: %v", err)
	}
	if _, err := expenses.Get(head, alien.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("head get after delete: want not_found, got %v", err)
	}
}

func TestSummaryAndLastRespectScope(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)
	seedUser(t, pool, userBob)

	aliceFood := mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 10000, Category: "food", SpentAt: "2026-10-19"})
	aliceTransport := mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 5000, Category: "transport", SpentAt: "2026-10-18"})
	bobFood := mustCreate(t, pool, userBob, service.CreateInput{AmountMinor: 7000, Category: "food", SpentAt: "2026-10-17"})

	// fixed created_at values make the Last ordering deterministic
	base := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	for i, id := range []int64{aliceFood.ID, aliceTransport.ID, bobFood.ID} {
		if _, err := pool.Exec(context.Background(), "UPDATE expenses SET created_at = $1 WHERE id = $2", base.Add(time.Duration(i)*time.Hour), id); err != nil {
			t.Fatalf("fix created_at: %v", err)
		}
	}

	expenses := service.NewExpenseService(pool)

	aliceSummary, err := expenses.Summary(service.WithUserScope(context.Background(), userAlice), service.SummaryInput{})
	if err != nil {
		t.Fatalf("alice summary: %v", err)
	}
	if aliceSummary.GrandTotal != 15000 {
		t.Fatalf("alice grand total: want 15000, got %d", aliceSummary.GrandTotal)
	}
	if len(aliceSummary.Rows) != 2 || aliceSummary.Rows[0].Category != "food" || aliceSummary.Rows[0].TotalMinor != 10000 {
		t.Fatalf("alice summary rows: %+v", aliceSummary.Rows)
	}

	bobSummary, err := expenses.Summary(service.WithUserScope(context.Background(), userBob), service.SummaryInput{})
	if err != nil {
		t.Fatalf("bob summary: %v", err)
	}
	if bobSummary.GrandTotal != 7000 || len(bobSummary.Rows) != 1 {
		t.Fatalf("bob summary must hold only his rows: %+v", bobSummary)
	}

	headSummary, err := expenses.Summary(service.WithFamilyHeadScope(context.Background(), nil), service.SummaryInput{})
	if err != nil {
		t.Fatalf("head summary: %v", err)
	}
	if headSummary.GrandTotal != 22000 {
		t.Fatalf("head grand total: want 22000, got %d", headSummary.GrandTotal)
	}

	last, err := expenses.Last(service.WithUserScope(context.Background(), userAlice))
	if err != nil {
		t.Fatalf("alice last: %v", err)
	}
	if last.ID != aliceTransport.ID {
		t.Fatalf("alice last must be her latest expense %d, got %d", aliceTransport.ID, last.ID)
	}
	headLast, err := expenses.Last(service.WithFamilyHeadScope(context.Background(), nil))
	if err != nil {
		t.Fatalf("head last: %v", err)
	}
	if headLast.ID != bobFood.ID {
		t.Fatalf("head last must be the globally latest expense %d, got %d", bobFood.ID, headLast.ID)
	}
}

func TestCreateValidation(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)

	expenses := service.NewExpenseService(pool)
	ctx := service.WithUserScope(context.Background(), userAlice)

	cases := []struct {
		name string
		in   service.CreateInput
		want error
	}{
		{"zero amount", service.CreateInput{AmountMinor: 0, Category: "food", SpentAt: "2026-10-19"}, domain.ErrInvalidAmount},
		{"negative amount", service.CreateInput{AmountMinor: -5, Category: "food", SpentAt: "2026-10-19"}, domain.ErrInvalidAmount},
		{"unknown category", service.CreateInput{AmountMinor: 100, Category: "кафе", SpentAt: "2026-10-19"}, domain.ErrInvalidCategory},
		{"wrong date format", service.CreateInput{AmountMinor: 100, Category: "food", SpentAt: "19.10.2026"}, domain.ErrInvalidDate},
		{"too long comment", service.CreateInput{AmountMinor: 100, Category: "food", SpentAt: "2026-10-19", Comment: strings.Repeat("ы", 501)}, domain.ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := expenses.Create(ctx, tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}

	created, err := expenses.Create(ctx, service.CreateInput{AmountMinor: 35050, Category: "Жильё", SpentAt: "2026-10-19", Comment: "rent"})
	if err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if created.Category != "housing" || created.AmountMinor != 35050 || created.Comment != "rent" {
		t.Fatalf("created expense mismatch: %+v", created)
	}
}

func TestPatchLeavesAbsentFieldsUnchanged(t *testing.T) {
	pool := newTestPool(t)
	seedUser(t, pool, userAlice)

	expenses := service.NewExpenseService(pool)
	ctx := service.WithUserScope(context.Background(), userAlice)

	e := mustCreate(t, pool, userAlice, service.CreateInput{AmountMinor: 10000, Category: "food", SpentAt: "2026-10-19", Comment: "кофе"})

	newAmount := int64(25000)
	patched, err := expenses.Patch(ctx, e.ID, service.PatchInput{AmountMinor: &newAmount})
	if err != nil {
		t.Fatalf("patch amount only: %v", err)
	}
	if patched.AmountMinor != 25000 || patched.Category != "food" || !patched.SpentAt.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) || patched.Comment != "кофе" {
		t.Fatalf("absent fields must stay unchanged: %+v", patched)
	}

	emptyComment := ""
	patched, err = expenses.Patch(ctx, e.ID, service.PatchInput{Comment: &emptyComment})
	if err != nil {
		t.Fatalf("clear comment: %v", err)
	}
	if patched.Comment != "" || patched.AmountMinor != 25000 {
		t.Fatalf("comment must be cleared, amount kept: %+v", patched)
	}

	zero := int64(0)
	if _, err := expenses.Patch(ctx, e.ID, service.PatchInput{AmountMinor: &zero}); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("patch zero amount: want invalid_amount, got %v", err)
	}
}
