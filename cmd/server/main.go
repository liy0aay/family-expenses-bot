package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	ogen "github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/config"
)

type stubHandler struct{}

func (stubHandler) ListUsers(context.Context) (*ogen.UserList, error) {
	return &ogen.UserList{}, nil
}

func (stubHandler) ListExpenses(context.Context, ogen.ListExpensesParams) (ogen.ListExpensesRes, error) {
	return &ogen.ExpenseList{}, nil
}

func (stubHandler) CreateExpense(context.Context, *ogen.ExpenseCreate) (ogen.CreateExpenseRes, error) {
	return &ogen.Expense{}, nil
}

func (stubHandler) GetExpense(context.Context, ogen.GetExpenseParams) (ogen.GetExpenseRes, error) {
	return &ogen.Expense{}, nil
}

func (stubHandler) PatchExpense(context.Context, *ogen.ExpensePatch, ogen.PatchExpenseParams) (ogen.PatchExpenseRes, error) {
	return &ogen.Expense{}, nil
}

func (stubHandler) DeleteExpense(context.Context, ogen.DeleteExpenseParams) (ogen.DeleteExpenseRes, error) {
	return &ogen.DeleteExpenseNoContent{}, nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ogenServer, err := ogen.NewServer(stubHandler{})
	if err != nil {
		slog.Error("ogen server creation failed", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", ogenServer))

	slog.Info("listening", "addr", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		slog.Error("http server failed", "err", err)
		os.Exit(1)
	}
}
