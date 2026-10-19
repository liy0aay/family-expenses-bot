package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/liy0aay/family-expenses-bot/internal/adapter/rest"
	ogen "github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/config"
	"github.com/liy0aay/family-expenses-bot/internal/service"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	pool, err := postgres.NewPool(ctx, cfg.Postgres.DSN())
	if err != nil {
		slog.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	version, err := postgres.Migrate(ctx, pool)
	if err != nil {
		slog.Error("migrations failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied", "version", version)

	ogenServer, err := ogen.NewServer(
		rest.NewHandlers(
			service.NewExpenseService(pool),
			service.NewUserService(pool),
			cfg.ExpensesListLimit,
		),
		ogen.WithErrorHandler(rest.NewErrorHandler()),
	)
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
