package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/liy0aay/family-expenses-bot/internal/agent"
	"github.com/liy0aay/family-expenses-bot/internal/clock"
	"github.com/liy0aay/family-expenses-bot/internal/domain"
	"github.com/liy0aay/family-expenses-bot/internal/service"
)

const defaultFindLimit = 10
const maxFindLimit = 10

type Tools struct {
	expenses *service.ExpenseService
	clk      clock.Clock
}

func New(expenses *service.ExpenseService, clk clock.Clock) *Tools {
	return &Tools{expenses: expenses, clk: clk}
}

// the orchestrator returns it to the LLM as {error:{code,message}}.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string {
	return e.Code + ": " + e.Message
}

func toolError(err error) *ToolError {
	code := "bad_request"
	switch {
	case errors.Is(err, domain.ErrInvalidAmount):
		code = "invalid_amount"
	case errors.Is(err, domain.ErrInvalidCategory):
		code = "invalid_category"
	case errors.Is(err, domain.ErrInvalidDate):
		code = "invalid_date"
	case errors.Is(err, domain.ErrNotFound):
		code = "not_found"
	case errors.Is(err, domain.ErrInternal):
		code = "internal"
	}
	return &ToolError{Code: code, Message: err.Error()}
}

func (t *Tools) Definitions() []agent.Tool {
	today := t.clk.Now().Format("2006-01-02")
	return []agent.Tool{
		{
			Type: "function",
			Function: agent.ToolFunction{
				Name:        "add_expense",
				Description: fmt.Sprintf("Создать трату. Текущая дата: %s.", today),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"amount_rub": map[string]any{"type": "number", "description": "Сумма в рублях, не более двух знаков после запятой"},
						"category":   categorySchema(),
						"spent_at":   map[string]any{"type": "string", "description": "Дата траты YYYY-MM-DD; относительные слова приведи к дате от текущей даты"},
						"comment":    map[string]any{"type": "string", "description": "Короткий комментарий, опционально"},
					},
					"required": []string{"amount_rub", "category", "spent_at"},
				},
			},
		},
		{
			Type: "function",
			Function: agent.ToolFunction{
				Name:        "find_expenses",
				Description: "Найти траты пользователя; сортировка по времени создания, свежие первыми. Текущая дата: " + today + ".",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"date_from": map[string]any{"type": "string", "description": "YYYY-MM-DD, включительно; опционально"},
						"date_to":   map[string]any{"type": "string", "description": "YYYY-MM-DD, включительно; опционально"},
						"category":  categorySchema(),
						"text":      map[string]any{"type": "string", "description": "Поиск по комментарию (например, «такси»); опционально"},
						"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": maxFindLimit, "description": "По умолчанию 10"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: agent.ToolFunction{
				Name:        "update_expense",
				Description: "Изменить трату по id; переданные поля меняются, остальные сохраняются.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expense_id": map[string]any{"type": "integer", "description": "ID траты (из контекста диалога или результата find_expenses)"},
						"amount_rub": map[string]any{"type": "number", "description": "Новая сумма в рублях; опционально"},
						"category":   categorySchema(),
						"spent_at":   map[string]any{"type": "string", "description": "YYYY-MM-DD; опционально"},
						"comment":    map[string]any{"type": "string", "description": "Опционально"},
					},
					"required": []string{"expense_id"},
				},
			},
		},
		{
			Type: "function",
			Function: agent.ToolFunction{
				Name:        "delete_expense",
				Description: "Удалить трату по id.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"expense_id": map[string]any{"type": "integer", "description": "ID траты"},
					},
					"required": []string{"expense_id"},
				},
			},
		},
		{
			Type: "function",
			Function: agent.ToolFunction{
				Name:        "get_summary",
				Description: "Сумма и число трат за период. Текущая дата: " + today + ".",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"date_from": map[string]any{"type": "string", "description": "YYYY-MM-DD, включительно"},
						"date_to":   map[string]any{"type": "string", "description": "YYYY-MM-DD, включительно"},
						"category":  categorySchema(),
					},
					"required": []string{"date_from", "date_to"},
				},
			},
		},
	}
}

func categorySchema() map[string]any {
	return map[string]any{
		"type": "string",
		"enum": domain.Categories,
	}
}

func (t *Tools) Dispatch(ctx context.Context, name, arguments string) (any, error) {
	args, err := agent.ParseToolArguments(arguments)
	if err != nil {
		return nil, err
	}
	switch name {
	case "add_expense":
		return t.add(ctx, args)
	case "find_expenses":
		return t.find(ctx, args)
	case "update_expense":
		return t.update(ctx, args)
	case "delete_expense":
		return t.remove(ctx, args)
	case "get_summary":
		return t.summary(ctx, args)
	default:
		return nil, toolError(fmt.Errorf("%w: unknown tool %q", domain.ErrValidation, name))
	}
}

func (t *Tools) add(ctx context.Context, args map[string]any) (any, error) {
	amount, err := amountArg(args)
	if err != nil {
		return nil, err
	}
	category, err := categoryArg(args)
	if err != nil {
		return nil, err
	}
	spentAt, err := stringArg(args, "spent_at", true)
	if err != nil {
		return nil, err
	}
	comment, err := stringArg(args, "comment", false)
	if err != nil {
		return nil, err
	}

	e, err := t.expenses.Create(ctx, service.CreateInput{
		AmountMinor: amount,
		Category:    category,
		SpentAt:     spentAt,
		Comment:     comment,
	})
	if err != nil {
		return nil, err
	}
	return expenseResult(e), nil
}

func (t *Tools) find(ctx context.Context, args map[string]any) (any, error) {
	dateFrom, err := stringArg(args, "date_from", false)
	if err != nil {
		return nil, err
	}
	dateTo, err := stringArg(args, "date_to", false)
	if err != nil {
		return nil, err
	}
	var category string
	if _, present := args["category"]; present {
		category, err = categoryArg(args)
		if err != nil {
			return nil, err
		}
	}
	text, err := stringArg(args, "text", false)
	if err != nil {
		return nil, err
	}
	limit := defaultFindLimit
	if raw, present := args["limit"]; present {
		n, ok := raw.(json.Number)
		if !ok {
			return nil, toolError(fmt.Errorf("%w: limit must be an integer", domain.ErrValidation))
		}
		parsed, err := n.Int64()
		if err != nil || parsed < 1 {
			return nil, toolError(fmt.Errorf("%w: limit must be a positive integer, got %s", domain.ErrValidation, n.String()))
		}
		limit = int(parsed)
		if limit > maxFindLimit {
			limit = maxFindLimit
		}
	}

	items, err := t.expenses.List(ctx, service.ListInput{
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Category: category,
		Text:     text,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, e := range items {
		out = append(out, expenseResult(e))
	}
	return map[string]any{"expenses": out, "count": len(out)}, nil
}

func (t *Tools) update(ctx context.Context, args map[string]any) (any, error) {
	id, err := intArg(args, "expense_id")
	if err != nil {
		return nil, err
	}
	in := service.PatchInput{}
	if _, present := args["amount_rub"]; present {
		amount, err := amountArg(args)
		if err != nil {
			return nil, err
		}
		in.AmountMinor = &amount
	}
	if _, present := args["category"]; present {
		category, err := categoryArg(args)
		if err != nil {
			return nil, err
		}
		in.Category = category
	}
	if _, present := args["spent_at"]; present {
		spentAt, err := stringArg(args, "spent_at", false)
		if err != nil {
			return nil, err
		}
		in.SpentAt = spentAt
	}
	if _, present := args["comment"]; present {
		comment, err := stringArg(args, "comment", false)
		if err != nil {
			return nil, err
		}
		in.Comment = &comment
	}

	e, err := t.expenses.Patch(ctx, id, in)
	if err != nil {
		return nil, err
	}
	return expenseResult(e), nil
}

func (t *Tools) remove(ctx context.Context, args map[string]any) (any, error) {
	id, err := intArg(args, "expense_id")
	if err != nil {
		return nil, err
	}
	e, err := t.expenses.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := t.expenses.Delete(ctx, id); err != nil {
		return nil, err
	}
	return expenseResult(e), nil
}

func (t *Tools) summary(ctx context.Context, args map[string]any) (any, error) {
	dateFrom, err := stringArg(args, "date_from", true)
	if err != nil {
		return nil, err
	}
	dateTo, err := stringArg(args, "date_to", true)
	if err != nil {
		return nil, err
	}
	var category string
	if _, present := args["category"]; present {
		category, err = categoryArg(args)
		if err != nil {
			return nil, err
		}
	}

	sum, err := t.expenses.Summary(ctx, service.SummaryInput{DateFrom: dateFrom, DateTo: dateTo})
	if err != nil {
		return nil, err
	}
	// rows are already grouped by category, so the category filter is a row selection
	var total, count int64
	if category == "" {
		for _, row := range sum.Rows {
			total += row.TotalMinor
			count += row.Count
		}
	} else {
		for _, row := range sum.Rows {
			if row.Category == category {
				total, count = row.TotalMinor, row.Count
			}
		}
	}
	return map[string]any{"total_minor": total, "count": count}, nil
}

func expenseResult(e service.Expense) map[string]any {
	name, _ := domain.CategoryRussianName(e.Category)
	return map[string]any{
		"id":            e.ID,
		"amount_minor":  e.AmountMinor,
		"amount_rub":    formatMinor(e.AmountMinor),
		"category":      e.Category,
		"category_name": name,
		"spent_at":      e.SpentAt.Format("2006-01-02"),
		"comment":       e.Comment,
	}
}

func formatMinor(minor int64) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return sign + strconv.FormatInt(minor/100, 10) + "." + fmt.Sprintf("%02d", minor%100)
}

// raw passes through untouched: RublesToKopecks owns the type gate, and LLMs
// send the amount as a JSON string despite the schema declaring number.
func amountArg(args map[string]any) (int64, error) {
	raw, present := args["amount_rub"]
	if !present {
		return 0, toolError(fmt.Errorf("%w: amount_rub is required", domain.ErrValidation))
	}
	minor, err := domain.RublesToKopecks(raw)
	if err != nil {
		return 0, toolError(err)
	}
	return minor, nil
}

func categoryArg(args map[string]any) (string, error) {
	raw, present := args["category"]
	if !present {
		return "", toolError(fmt.Errorf("%w: category is required", domain.ErrValidation))
	}
	s, ok := raw.(string)
	if !ok {
		return "", toolError(fmt.Errorf("%w: category must be a string, got %T", domain.ErrInvalidCategory, raw))
	}
	slug, err := domain.NormalizeCategory(s)
	if err != nil {
		return "", toolError(err)
	}
	return slug, nil
}

func intArg(args map[string]any, key string) (int64, error) {
	raw, present := args[key]
	if !present {
		return 0, toolError(fmt.Errorf("%w: %s is required", domain.ErrValidation, key))
	}
	number, ok := raw.(json.Number)
	if !ok {
		return 0, toolError(fmt.Errorf("%w: %s must be an integer, got %T", domain.ErrValidation, key, raw))
	}
	id, err := number.Int64()
	if err != nil {
		return 0, toolError(fmt.Errorf("%w: %s must be an integer, got %s", domain.ErrValidation, key, number.String()))
	}
	return id, nil
}

func stringArg(args map[string]any, key string, required bool) (string, error) {
	value, present, err := optionalStringArg(args, key)
	if err != nil {
		return "", err
	}
	if !present && required {
		return "", toolError(fmt.Errorf("%w: %s is required", domain.ErrValidation, key))
	}
	return value, nil
}

func optionalStringArg(args map[string]any, key string) (string, bool, error) {
	raw, present := args[key]
	if !present {
		return "", false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", false, toolError(fmt.Errorf("%w: %s must be a string, got %T", domain.ErrValidation, key, raw))
	}
	return s, true, nil
}
