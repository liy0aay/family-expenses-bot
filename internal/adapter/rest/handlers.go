package rest

import (
	"context"
	"net/http"
	"strconv"

	"github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/domain"
	"github.com/liy0aay/family-expenses-bot/internal/service"
)

type Handlers struct {
	expenses  *service.ExpenseService
	users     *service.UserService
	listLimit int
}

func NewHandlers(expenses *service.ExpenseService, users *service.UserService, listLimit int) *Handlers {
	return &Handlers{expenses: expenses, users: users, listLimit: listLimit}
}

func (h *Handlers) ListUsers(ctx context.Context) (*ogen.UserList, error) {
	users, err := h.users.ListUsers(service.WithFamilyHeadScope(ctx, nil))
	if err != nil {
		return nil, err
	}
	items := make([]ogen.User, 0, len(users))
	for _, u := range users {
		items = append(items, ogen.User{
			ID:        u.ID,
			Username:  ogen.OptNilString{Value: u.Username, Set: true, Null: u.Username == ""},
			FirstName: u.FirstName,
		})
	}
	return &ogen.UserList{Items: items}, nil
}

func (h *Handlers) ListExpenses(ctx context.Context, params ogen.ListExpensesParams) (ogen.ListExpensesRes, error) {
	in := service.ListInput{Limit: h.listLimit}
	if params.Limit.Set {
		in.Limit = params.Limit.Value
	}
	var filter *int64
	if params.UserID.Set {
		filter = &params.UserID.Value
	}
	if params.DateFrom.Set {
		in.DateFrom = params.DateFrom.Value.Format("2006-01-02")
	}
	if params.DateTo.Set {
		in.DateTo = params.DateTo.Value.Format("2006-01-02")
	}
	if params.Category.Set {
		in.Category = params.Category.Value
	}

	list, err := h.expenses.List(service.WithFamilyHeadScope(ctx, filter), in)
	if err != nil {
		return errorResponse(err), nil
	}
	names, err := h.userNames(ctx)
	if err != nil {
		return errorResponse(err), nil
	}
	items := make([]ogen.Expense, 0, len(list))
	for _, e := range list {
		items = append(items, *toOgenExpense(e, names))
	}
	return &ogen.ExpenseList{Items: items}, nil
}

func (h *Handlers) CreateExpense(ctx context.Context, req *ogen.ExpenseCreate) (ogen.CreateExpenseRes, error) {
	amount, err := domain.RublesToKopecks(req.AmountRub)
	if err != nil {
		return errorResponse(err), nil
	}
	in := service.CreateInput{
		AmountMinor: amount,
		Category:    req.Category,
		SpentAt:     req.SpentAt.Format("2006-01-02"),
	}
	if req.Comment.Set {
		in.Comment = req.Comment.Value
	}

	e, err := h.expenses.Create(service.WithFamilyHeadScope(ctx, &req.UserID), in)
	if err != nil {
		return errorResponse(err), nil
	}
	names, err := h.userNames(ctx)
	if err != nil {
		return errorResponse(err), nil
	}
	return toOgenExpense(e, names), nil
}

func (h *Handlers) GetExpense(ctx context.Context, params ogen.GetExpenseParams) (ogen.GetExpenseRes, error) {
	e, err := h.expenses.Get(service.WithFamilyHeadScope(ctx, nil), params.ExpenseID)
	if err != nil {
		return errorResponse(err), nil
	}
	names, err := h.userNames(ctx)
	if err != nil {
		return errorResponse(err), nil
	}
	return toOgenExpense(e, names), nil
}

func (h *Handlers) PatchExpense(ctx context.Context, req *ogen.ExpensePatch, params ogen.PatchExpenseParams) (ogen.PatchExpenseRes, error) {
	in := service.PatchInput{}
	if req.AmountRub.Set {
		amount, err := domain.RublesToKopecks(req.AmountRub.Value)
		if err != nil {
			return patchErrorResponse(err), nil
		}
		in.AmountMinor = &amount
	}
	if req.Category.Set {
		in.Category = req.Category.Value
	}
	if req.SpentAt.Set {
		in.SpentAt = req.SpentAt.Value.Format("2006-01-02")
	}
	if req.Comment.Set {
		in.Comment = &req.Comment.Value
	}

	e, err := h.expenses.Patch(service.WithFamilyHeadScope(ctx, nil), params.ExpenseID, in)
	if err != nil {
		return patchErrorResponse(err), nil
	}
	names, err := h.userNames(ctx)
	if err != nil {
		return patchErrorResponse(err), nil
	}
	return toOgenExpense(e, names), nil
}

func (h *Handlers) DeleteExpense(ctx context.Context, params ogen.DeleteExpenseParams) (ogen.DeleteExpenseRes, error) {
	if err := h.expenses.Delete(service.WithFamilyHeadScope(ctx, nil), params.ExpenseID); err != nil {
		return errorResponse(err), nil
	}
	return &ogen.DeleteExpenseNoContent{}, nil
}

// the wire status is fixed by the generated encoder per union variant
// (create/list → 400, get/delete → 404); internal errors leave with the
// operation's declared status, the code field stays internal.
func errorResponse(err error) *ogen.Error {
	payload, _ := errorPayload(err)
	return &ogen.Error{Error: payload}
}

func patchErrorResponse(err error) ogen.PatchExpenseRes {
	payload, status := errorPayload(err)
	if status == http.StatusNotFound {
		return &ogen.PatchExpenseNotFound{Error: payload}
	}
	return &ogen.PatchExpenseBadRequest{Error: payload}
}

func (h *Handlers) userNames(ctx context.Context) (map[int64]string, error) {
	users, err := h.users.ListUsers(service.WithFamilyHeadScope(ctx, nil))
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.FirstName
		if names[u.ID] == "" {
			names[u.ID] = u.Username
		}
	}
	return names, nil
}

func toOgenExpense(e service.Expense, names map[int64]string) *ogen.Expense {
	name := names[e.UserID]
	if name == "" {
		name = strconv.FormatInt(e.UserID, 10)
	}
	categoryName, _ := domain.CategoryRussianName(e.Category)
	return &ogen.Expense{
		ID:           e.ID,
		UserID:       e.UserID,
		UserName:     name,
		AmountMinor:  e.AmountMinor,
		Category:     ogen.ExpenseCategory(e.Category),
		CategoryName: ogen.ExpenseCategoryName(categoryName),
		SpentAt:      e.SpentAt,
		Comment:      ogen.OptNilString{Value: e.Comment, Set: true, Null: e.Comment == ""},
		CreatedAt:    e.CreatedAt,
		UpdatedAt:    e.UpdatedAt,
	}
}
