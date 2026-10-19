package service

import (
	"context"
	"fmt"

	"github.com/liy0aay/family-expenses-bot/internal/domain"
)

type scopeKind int

const (
	scopeUser scopeKind = iota
	scopeFamilyHead
)

type scope struct {
	kind       scopeKind
	userID     int64  // scopeUser
	userFilter *int64 // scopeFamilyHead
}

type scopeKey struct{}

func WithUserScope(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope{kind: scopeUser, userID: userID})
}

func WithFamilyHeadScope(ctx context.Context, userFilter *int64) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope{kind: scopeFamilyHead, userFilter: userFilter})
}

func scopeFromContext(ctx context.Context) (scope, error) {
	s, ok := ctx.Value(scopeKey{}).(scope)
	if !ok {
		return scope{}, fmt.Errorf("%w: scope not set", domain.ErrInternal)
	}
	return s, nil
}

// rowUserFilter builds the user_id predicate for row access: UserScope is
// materialized as a strict filter (bot sees only its own rows), FamilyHeadScope
// passes its optional filter through.
func (s scope) rowUserFilter() *int64 {
	if s.kind == scopeUser {
		id := s.userID
		return &id
	}
	return s.userFilter
}

// createUserID resolves the owner of a new expense: the bot user itself, or the
// family member the panel chose (its user_id travels inside the head scope filter).
func (s scope) createUserID() (int64, error) {
	if s.kind == scopeUser {
		return s.userID, nil
	}
	if s.userFilter == nil {
		return 0, fmt.Errorf("%w: user_id is required to create an expense", domain.ErrValidation)
	}
	return *s.userFilter, nil
}
