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

// Seed fills an empty DB once; reruns must not duplicate demo data.
func TestSeedIdempotent(t *testing.T) {
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

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if err := postgres.Seed(ctx, pool, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	users, expenses := counts(ctx, t, pool)
	if users != 3 {
		t.Fatalf("users: got %d, want 3", users)
	}
	if expenses != 12 {
		t.Fatalf("expenses: got %d, want 12", expenses)
	}

	monthStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows, err := pool.Query(ctx, `SELECT spent_at FROM expenses`)
	if err != nil {
		t.Fatalf("select spent_at: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d pgtype.Date
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan spent_at: %v", err)
		}
		if d.Time.Before(monthStart) || d.Time.After(now) {
			t.Fatalf("spent_at %s outside [%s, %s]",
				d.Time.Format("2006-01-02"), monthStart.Format("2006-01-02"), now.Format("2006-01-02"))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if err := postgres.Seed(ctx, pool, now); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	usersAfter, expensesAfter := counts(ctx, t, pool)
	if usersAfter != users || expensesAfter != expenses {
		t.Fatalf("re-seed changed counts: users %d->%d, expenses %d->%d",
			users, usersAfter, expenses, expensesAfter)
	}
}

func counts(ctx context.Context, t *testing.T, pool *pgxpool.Pool) (users, expenses int64) {
	t.Helper()
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM users),
		(SELECT COUNT(*) FROM expenses)`).Scan(&users, &expenses); err != nil {
		t.Fatalf("counts: %v", err)
	}
	return users, expenses
}
