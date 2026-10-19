package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/liy0aay/family-expenses-bot/internal/storage/migrations"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	// goose is a package-level global; wiring its logger into slog keeps
	// migration output in the same stream as the rest of the server logs.
	goose.SetLogger(gooseLogger{})
	goose.SetBaseFS(migrations.FS)

	if err := goose.SetDialect("postgres"); err != nil {
		return 0, fmt.Errorf("goose dialect: %w", err)
	}

	// goose expects database/sql; OpenDBFromPool lends the existing pool
	// as a *sql.DB instead of opening a second connection.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() {
		if err := sqlDB.Close(); err != nil {
			slog.Warn("sql.DB close failed", "err", err)
		}
	}()

	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return 0, fmt.Errorf("goose up: %w", err)
	}
	version, err := goose.GetDBVersionContext(ctx, sqlDB)
	if err != nil {
		return 0, fmt.Errorf("goose version: %w", err)
	}
	return version, nil
}

type gooseLogger struct{}

func (gooseLogger) Fatalf(format string, v ...any) { slog.Error(fmt.Sprintf(format, v...)) }
func (gooseLogger) Printf(format string, v ...any) { slog.Info(fmt.Sprintf(format, v...)) }
