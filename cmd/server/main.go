package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/liy0aay/family-expenses-bot/internal/adapter/rest"
	ogen "github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/adapter/telegram"
	"github.com/liy0aay/family-expenses-bot/internal/agent"
	"github.com/liy0aay/family-expenses-bot/internal/agent/memory"
	"github.com/liy0aay/family-expenses-bot/internal/agent/tools"
	"github.com/liy0aay/family-expenses-bot/internal/clock"
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

	clk := clock.Real{}
	if err := postgres.Seed(ctx, pool, clk.Now()); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}

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

	expenses := service.NewExpenseService(pool)
	dialogAgent := agent.New(
		agent.NewClient(cfg.LLM, clk),
		tools.New(expenses, clk),
		memory.New(cfg.DialogWindow, cfg.DialogTTL, clk),
		clk,
	)
	if cfg.TelegramBotToken == "" {
		slog.Warn("starting in Web-API only mode")
	} else {
		telegram.New(cfg.TelegramBotToken, dialogAgent, service.NewUserService(pool)).Start(ctx)
	}

	slog.Info("listening", "addr", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		slog.Error("http server failed", "err", err)
		os.Exit(1)
	}
}
