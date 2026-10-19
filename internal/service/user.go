package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/domain"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

type User struct {
	ID        int64
	Username  string
	FirstName string
}

type UserService struct {
	q *postgres.Queries
}

func NewUserService(pool *pgxpool.Pool) *UserService {
	return &UserService{q: postgres.New(pool)}
}

func (s *UserService) UpsertUser(ctx context.Context, id int64, username, firstName string) (User, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return User{}, err
	}
	// under UserScope the profile must be the scope owner's own; a mismatch is a caller bug
	if sc.kind == scopeUser && sc.userID != id {
		return User{}, fmt.Errorf("%w: upsert user %d under user scope %d", domain.ErrInternal, id, sc.userID)
	}
	row, err := s.q.UpsertUser(ctx, postgres.UpsertUserParams{
		ID:        id,
		Username:  pgtype.Text{String: username, Valid: username != ""},
		FirstName: firstName,
	})
	if err != nil {
		return User{}, fmt.Errorf("%w: upsert user: %w", domain.ErrInternal, err)
	}
	return toUser(row), nil
}

func (s *UserService) ListUsers(ctx context.Context) ([]User, error) {
	sc, err := scopeFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: list users: %w", domain.ErrInternal, err)
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		// the roster serves the panel; under UserScope the bot gets only its own row
		if sc.kind == scopeUser && row.ID != sc.userID {
			continue
		}
		users = append(users, toUser(row))
	}
	return users, nil
}

func toUser(row postgres.User) User {
	return User{
		ID:        row.ID,
		Username:  row.Username.String,
		FirstName: row.FirstName,
	}
}
