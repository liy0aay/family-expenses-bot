package domain

import "errors"

var (
	ErrInvalidAmount   = errors.New("invalid_amount")
	ErrInvalidCategory = errors.New("invalid_category")
	ErrInvalidDate     = errors.New("invalid_date")
	ErrNotFound        = errors.New("not_found")
	ErrValidation      = errors.New("bad_request")
	ErrInternal        = errors.New("internal")
)
