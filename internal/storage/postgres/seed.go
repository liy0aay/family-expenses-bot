package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// demo dataset for the panel (quickstart scenario 1); fixed IDs keep reruns predictable
var seedUsers = []UpsertUserParams{
	{ID: 101, FirstName: "Алиса"},
	{ID: 102, FirstName: "Борис"},
	{ID: 103, FirstName: "Мария"},
}

type seedExpense struct {
	userID      int64
	daysBack    int
	amountMinor int64
	category    string
	comment     string
}

// dates are clamped to the month start, so the set fits any day of the month
var seedExpenses = []seedExpense{
	{userID: 101, daysBack: 0, amountMinor: 35000, category: "food", comment: "Кофе с круассаном"},
	{userID: 102, daysBack: 0, amountMinor: 89990, category: "shopping", comment: "Кроссовки для сына"},
	{userID: 101, daysBack: 1, amountMinor: 120000, category: "transport", comment: "Такси до аэропорта"},
	{userID: 103, daysBack: 2, amountMinor: 230050, category: "health", comment: "Стоматолог, осмотр"},
	{userID: 102, daysBack: 4, amountMinor: 450000, category: "housing", comment: "Коммунальные платежи"},
	{userID: 103, daysBack: 6, amountMinor: 150000, category: "entertainment", comment: "Кино всей семьёй"},
	{userID: 101, daysBack: 8, amountMinor: 78020, category: "food", comment: "Продукты на неделю"},
	{userID: 102, daysBack: 10, amountMinor: 349900, category: "shopping", comment: "Зимняя куртка"},
	{userID: 103, daysBack: 12, amountMinor: 99000, category: "other", comment: "Химчистка ковра"},
	{userID: 101, daysBack: 14, amountMinor: 55000, category: "transport", comment: "Пополнение транспортной карты"},
	{userID: 102, daysBack: 16, amountMinor: 1250000, category: "health", comment: "Курс витаминов"},
	{userID: 103, daysBack: 18, amountMinor: 420000, category: "entertainment", comment: "Абонемент в бассейн"},
}

// Seed fills an empty database with demo users and expenses of the current month;
// skipped when any user exists. spent_at derives from now (Clock.Now() by the caller),
// created_at/updated_at are set by the DB like in CreateExpense.
func Seed(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	var users int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return fmt.Errorf("seed users count: %w", err)
	}
	if users > 0 {
		slog.Info("seed skipped, users exist")
		return nil
	}

	q := New(pool)
	for _, u := range seedUsers {
		if _, err := q.UpsertUser(ctx, u); err != nil {
			return fmt.Errorf("seed user %d: %w", u.ID, err)
		}
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for _, e := range seedExpenses {
		spentAt := now.AddDate(0, 0, -e.daysBack)
		if spentAt.Before(monthStart) {
			spentAt = monthStart
		}
		if _, err := q.CreateExpense(ctx, CreateExpenseParams{
			UserID:      e.userID,
			AmountMinor: e.amountMinor,
			Category:    e.category,
			SpentAt:     pgtype.Date{Time: spentAt, Valid: true},
			Comment:     pgtype.Text{String: e.comment, Valid: true},
		}); err != nil {
			return fmt.Errorf("seed expense for user %d: %w", e.userID, err)
		}
	}

	slog.Info("seed completed", "users", len(seedUsers), "expenses", len(seedExpenses))
	return nil
}
