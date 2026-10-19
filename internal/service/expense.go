package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/domain"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

const maxCommentLen = 500

type Expense struct {
	ID          int64
	UserID      int64
	AmountMinor int64
	Category    string
	SpentAt     time.Time
	Comment     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateInput struct {
	AmountMinor int64
	Category    string
	SpentAt     string // YYYY-MM-DD
	Comment     string
}

// Absent fields (nil / empty string) leave the stored value unchanged,
// matching the sqlc narg/COALESCE update; a non-nil Comment pointing at ""
// clears the comment.
type PatchInput struct {
	AmountMinor *int64
	Category    string
	SpentAt     string
	Comment     *string
}

type ListInput struct {
	DateFrom string // YYYY-MM-DD, empty = unbounded
	DateTo   string
	Category string
	Limit    int
}

type SummaryInput struct {
	DateFrom string
	DateTo   string
}

type SummaryRow struct {
	Category   string
	TotalMinor int64
	Count      int64
}

type Summary struct {
	Rows       []SummaryRow
	GrandTotal int64
}

type ExpenseService struct {
	q *postgres.Queries
}

func NewExpenseService(pool *pgxpool.Pool) *ExpenseService {
	return &ExpenseService{q: postgres.New(pool)}
}

func (s *ExpenseService) Create(ctx context.Context, in CreateInput) (Expense, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return Expense{}, err
	}
	userID, err := sc.createUserID()
	if err != nil {
		return Expense{}, err
	}
	if in.AmountMinor <= 0 {
		return Expense{}, fmt.Errorf("%w: amount_minor must be positive, got %d", domain.ErrInvalidAmount, in.AmountMinor)
	}
	category, err := domain.NormalizeCategory(in.Category)
	if err != nil {
		return Expense{}, err
	}
	spentAt, err := parseDate(in.SpentAt)
	if err != nil {
		return Expense{}, err
	}
	if utf8.RuneCountInString(in.Comment) > maxCommentLen {
		return Expense{}, fmt.Errorf("%w: comment is longer than %d characters", domain.ErrValidation, maxCommentLen)
	}

	row, err := s.q.CreateExpense(ctx, postgres.CreateExpenseParams{
		UserID:      userID,
		AmountMinor: in.AmountMinor,
		Category:    category,
		SpentAt:     pgtype.Date{Time: spentAt, Valid: true},
		Comment:     pgtype.Text{String: in.Comment, Valid: in.Comment != ""},
	})
	if err != nil {
		return Expense{}, fmt.Errorf("%w: create expense: %w", domain.ErrInternal, err)
	}
	return toExpense(row), nil
}

func (s *ExpenseService) List(ctx context.Context, in ListInput) ([]Expense, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if in.Limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive, got %d", domain.ErrValidation, in.Limit)
	}
	dateFrom, err := dateFilter(in.DateFrom)
	if err != nil {
		return nil, err
	}
	dateTo, err := dateFilter(in.DateTo)
	if err != nil {
		return nil, err
	}
	category, err := categoryFilter(in.Category)
	if err != nil {
		return nil, err
	}

	rows, err := s.q.ListExpenses(ctx, postgres.ListExpensesParams{
		UserID:   nullableInt8(sc.rowUserFilter()),
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Category: category,
		Lim:      int64(in.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list expenses: %w", domain.ErrInternal, err)
	}
	expenses := make([]Expense, 0, len(rows))
	for _, row := range rows {
		expenses = append(expenses, toExpense(row))
	}
	return expenses, nil
}

func (s *ExpenseService) Get(ctx context.Context, id int64) (Expense, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return Expense{}, err
	}
	row, err := s.q.GetExpense(ctx, postgres.GetExpenseParams{
		ID:     id,
		UserID: nullableInt8(sc.rowUserFilter()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, fmt.Errorf("%w: expense %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return Expense{}, fmt.Errorf("%w: get expense: %w", domain.ErrInternal, err)
	}
	return toExpense(row), nil
}

func (s *ExpenseService) Patch(ctx context.Context, id int64, in PatchInput) (Expense, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return Expense{}, err
	}

	var amount pgtype.Int8
	if in.AmountMinor != nil {
		if *in.AmountMinor <= 0 {
			return Expense{}, fmt.Errorf("%w: amount_minor must be positive, got %d", domain.ErrInvalidAmount, *in.AmountMinor)
		}
		amount = pgtype.Int8{Int64: *in.AmountMinor, Valid: true}
	}
	var category pgtype.Text
	if strings.TrimSpace(in.Category) != "" {
		slug, err := domain.NormalizeCategory(in.Category)
		if err != nil {
			return Expense{}, err
		}
		category = pgtype.Text{String: slug, Valid: true}
	}
	var spentAt pgtype.Date
	if strings.TrimSpace(in.SpentAt) != "" {
		t, err := parseDate(in.SpentAt)
		if err != nil {
			return Expense{}, err
		}
		spentAt = pgtype.Date{Time: t, Valid: true}
	}
	var comment pgtype.Text
	if in.Comment != nil {
		if utf8.RuneCountInString(*in.Comment) > maxCommentLen {
			return Expense{}, fmt.Errorf("%w: comment is longer than %d characters", domain.ErrValidation, maxCommentLen)
		}
		// an explicitly provided empty string clears the comment; NULL would mean "keep" to COALESCE
		comment = pgtype.Text{String: *in.Comment, Valid: true}
	}

	row, err := s.q.UpdateExpense(ctx, postgres.UpdateExpenseParams{
		AmountMinor: amount,
		Category:    category,
		SpentAt:     spentAt,
		Comment:     comment,
		ID:          id,
		UserID:      nullableInt8(sc.rowUserFilter()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, fmt.Errorf("%w: expense %d", domain.ErrNotFound, id)
	}
	if err != nil {
		return Expense{}, fmt.Errorf("%w: update expense: %w", domain.ErrInternal, err)
	}
	return toExpense(row), nil
}

func (s *ExpenseService) Delete(ctx context.Context, id int64) error {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return err
	}
	deleted, err := s.q.DeleteExpense(ctx, postgres.DeleteExpenseParams{
		ID:     id,
		UserID: nullableInt8(sc.rowUserFilter()),
	})
	if err != nil {
		return fmt.Errorf("%w: delete expense: %w", domain.ErrInternal, err)
	}
	if deleted == 0 {
		return fmt.Errorf("%w: expense %d", domain.ErrNotFound, id)
	}
	return nil
}

func (s *ExpenseService) Summary(ctx context.Context, in SummaryInput) (Summary, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	dateFrom, err := dateFilter(in.DateFrom)
	if err != nil {
		return Summary{}, err
	}
	dateTo, err := dateFilter(in.DateTo)
	if err != nil {
		return Summary{}, err
	}

	rows, err := s.q.SummaryByCategory(ctx, postgres.SummaryByCategoryParams{
		UserID:   nullableInt8(sc.rowUserFilter()),
		DateFrom: dateFrom,
		DateTo:   dateTo,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("%w: summary: %w", domain.ErrInternal, err)
	}
	out := Summary{Rows: make([]SummaryRow, 0, len(rows))}
	for _, row := range rows {
		out.Rows = append(out.Rows, SummaryRow{
			Category:   row.Category,
			TotalMinor: row.TotalMinor,
			Count:      row.ExpensesCount,
		})
		out.GrandTotal += row.TotalMinor
	}
	return out, nil
}

func (s *ExpenseService) Last(ctx context.Context) (Expense, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return Expense{}, err
	}
	row, err := s.q.LastExpense(ctx, nullableInt8(sc.rowUserFilter()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, fmt.Errorf("%w: no expenses yet", domain.ErrNotFound)
	}
	if err != nil {
		return Expense{}, fmt.Errorf("%w: last expense: %w", domain.ErrInternal, err)
	}
	return toExpense(row), nil
}

func parseDate(raw string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q is not YYYY-MM-DD", domain.ErrInvalidDate, raw)
	}
	return t, nil
}

func dateFilter(raw string) (pgtype.Date, error) {
	if strings.TrimSpace(raw) == "" {
		return pgtype.Date{}, nil
	}
	t, err := parseDate(raw)
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func categoryFilter(raw string) (pgtype.Text, error) {
	if strings.TrimSpace(raw) == "" {
		return pgtype.Text{}, nil
	}
	slug, err := domain.NormalizeCategory(raw)
	if err != nil {
		return pgtype.Text{}, err
	}
	return pgtype.Text{String: slug, Valid: true}, nil
}

func nullableInt8(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

func toExpense(row postgres.Expense) Expense {
	return Expense{
		ID:          row.ID,
		UserID:      row.UserID,
		AmountMinor: row.AmountMinor,
		Category:    row.Category,
		SpentAt:     row.SpentAt.Time,
		Comment:     row.Comment.String,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}
