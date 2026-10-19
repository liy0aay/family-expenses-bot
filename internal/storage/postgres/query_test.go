package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

// ListExpenses must order by spent_at (the expense date), not by insertion time:
// a backdated entry surfaces at its date, not where it was typed.
func TestListExpensesOrdersBySpentAt(t *testing.T) {
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

	q := postgres.New(pool)
	const userID = 201
	if _, err := q.UpsertUser(ctx, postgres.UpsertUserParams{ID: userID, FirstName: "Order"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	create := func(spentAt time.Time) postgres.Expense {
		t.Helper()
		row, err := q.CreateExpense(ctx, postgres.CreateExpenseParams{
			UserID:      userID,
			AmountMinor: 10000,
			Category:    "food",
			SpentAt:     pgtype.Date{Time: spentAt, Valid: true},
		})
		if err != nil {
			t.Fatalf("create expense spent_at=%s: %v", spentAt.Format("2006-01-02"), err)
		}
		return row
	}

	a := create(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)) // inserted first
	b := create(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	c := create(time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))

	rows, err := q.ListExpenses(ctx, postgres.ListExpensesParams{
		UserID: pgtype.Int8{Int64: userID, Valid: true},
		Lim:    10,
	})
	if err != nil {
		t.Fatalf("list expenses: %v", err)
	}

	want := []int64{b.ID, a.ID, c.ID}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		if rows[i].ID != w {
			t.Fatalf("row %d: got id %d, want id %d (order must be B, A, C)", i, rows[i].ID, w)
		}
	}
}
