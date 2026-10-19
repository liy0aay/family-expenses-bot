package rest

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-faster/jx"

	"github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/domain"
)

func errorPayload(err error) (ogen.ErrorError, int) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeInvalidAmount, Message: err.Error()}, http.StatusBadRequest
	case errors.Is(err, domain.ErrInvalidCategory):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeInvalidCategory, Message: err.Error()}, http.StatusBadRequest
	case errors.Is(err, domain.ErrInvalidDate):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeInvalidDate, Message: err.Error()}, http.StatusBadRequest
	case errors.Is(err, domain.ErrValidation):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeBadRequest, Message: err.Error()}, http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeNotFound, Message: err.Error()}, http.StatusNotFound
	case errors.Is(err, domain.ErrInternal):
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeInternal, Message: err.Error()}, http.StatusInternalServerError
	default:
		return ogen.ErrorError{Code: ogen.ErrorErrorCodeBadRequest, Message: err.Error()}, http.StatusBadRequest
	}
}

func writeError(w http.ResponseWriter, payload ogen.ErrorError, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	response := ogen.Error{Error: payload}
	e := new(jx.Encoder)
	response.Encode(e)
	_, _ = e.WriteTo(w)
}

// ogen routes decode/validation failures here before any handler runs.
func NewErrorHandler() ogen.ErrorHandler {
	return func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
		payload, status := errorPayload(err)
		writeError(w, payload, status)
	}
}
