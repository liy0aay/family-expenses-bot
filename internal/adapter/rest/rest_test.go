package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liy0aay/family-expenses-bot/internal/adapter/rest"
	ogen "github.com/liy0aay/family-expenses-bot/internal/adapter/rest/ogen"
	"github.com/liy0aay/family-expenses-bot/internal/service"
	"github.com/liy0aay/family-expenses-bot/internal/storage/postgres"
)

const (
	aliceID = 101
	bobID   = 102
)

type expenseResponse struct {
	ID           int64   `json:"id"`
	UserID       int64   `json:"user_id"`
	UserName     string  `json:"user_name"`
	AmountMinor  int64   `json:"amount_minor"`
	Category     string  `json:"category"`
	CategoryName string  `json:"category_name"`
	SpentAt      string  `json:"spent_at"`
	Comment      *string `json:"comment"`
}

type listResponse struct {
	Items []expenseResponse `json:"items"`
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newTestEnv(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE users, expenses RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	server, err := ogen.NewServer(
		rest.NewHandlers(service.NewExpenseService(pool), service.NewUserService(pool), 2),
		ogen.WithErrorHandler(rest.NewErrorHandler()),
	)
	if err != nil {
		t.Fatalf("ogen server: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", server))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool, id int64, username, firstName string) {
	t.Helper()

	users := service.NewUserService(pool)
	_, err := users.UpsertUser(service.WithFamilyHeadScope(context.Background(), nil), id, username, firstName)
	if err != nil {
		t.Fatalf("seed user %d: %v", id, err)
	}
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, body string) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, ts.URL+"/api/v1"+path, reader)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response: %v", err)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, data
}

func decodeError(t *testing.T, status int, data []byte) errorBody {
	t.Helper()

	var e errorBody
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("status %d: decode error body %q: %v", status, data, err)
	}
	return e
}

func createExpense(t *testing.T, ts *httptest.Server, body string) expenseResponse {
	t.Helper()

	status, data := doRequest(t, ts, http.MethodPost, "/expenses", body)
	if status != http.StatusCreated {
		t.Fatalf("create %q: status %d: %s", body, status, data)
	}
	var e expenseResponse
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("decode created expense: %v", err)
	}
	return e
}

func listExpenses(t *testing.T, ts *httptest.Server, query string) []expenseResponse {
	t.Helper()

	status, data := doRequest(t, ts, http.MethodGet, "/expenses"+query, "")
	if status != http.StatusOK {
		t.Fatalf("list %q: status %d: %s", query, status, data)
	}
	var l listResponse
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return l.Items
}

func TestExpensesCRUD(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")

	created := createExpense(t, ts, `{"user_id":101,"amount_rub":"350.50","category":"еда","spent_at":"2026-10-19","comment":"обед"}`)
	if created.AmountMinor != 35050 || created.Category != "food" || created.CategoryName != "еда" {
		t.Fatalf("created mismatch: %+v", created)
	}
	if created.UserID != aliceID || created.UserName != "Алиса" || created.SpentAt != "2026-10-19" || *created.Comment != "обед" {
		t.Fatalf("created fields mismatch: %+v", created)
	}

	status, data := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/expenses/%d", created.ID), "")
	if status != http.StatusOK {
		t.Fatalf("get: status %d: %s", status, data)
	}
	var got expenseResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AmountMinor != 35050 || got.CategoryName != "еда" {
		t.Fatalf("fetched mismatch: %+v", got)
	}

	status, data = doRequest(t, ts, http.MethodPatch, fmt.Sprintf("/expenses/%d", created.ID), `{"amount_rub":"450"}`)
	if status != http.StatusOK {
		t.Fatalf("patch: status %d: %s", status, data)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode patched: %v", err)
	}
	if got.AmountMinor != 45000 || got.Category != "food" || *got.Comment != "обед" {
		t.Fatalf("patched mismatch: %+v", got)
	}

	status, data = doRequest(t, ts, http.MethodPatch, fmt.Sprintf("/expenses/%d", created.ID), `{"comment":null}`)
	if status != http.StatusOK {
		t.Fatalf("patch comment null: status %d: %s", status, data)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Comment != nil {
		t.Fatalf("comment must be cleared, got %q", *got.Comment)
	}

	status, _ = doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/expenses/%d", created.ID), "")
	if status != http.StatusNoContent {
		t.Fatalf("delete: status %d", status)
	}
	status, data = doRequest(t, ts, http.MethodGet, fmt.Sprintf("/expenses/%d", created.ID), "")
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: status %d: %s", status, data)
	}
	if e := decodeError(t, status, data); e.Error.Code != "not_found" {
		t.Fatalf("want not_found, got %q", e.Error.Code)
	}
}

func TestExpensesFilters(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")
	seedUser(t, pool, bobID, "bob", "Борис")

	aliceFood := createExpense(t, ts, `{"user_id":101,"amount_rub":"100","category":"еда","spent_at":"2026-10-19"}`)
	aliceTransport := createExpense(t, ts, `{"user_id":101,"amount_rub":"200","category":"транспорт","spent_at":"2026-10-15"}`)
	bobFood := createExpense(t, ts, `{"user_id":102,"amount_rub":"300","category":"еда","spent_at":"2026-10-10"}`)
	bobHousing := createExpense(t, ts, `{"user_id":102,"amount_rub":"400","category":"жильё","spent_at":"2026-10-20"}`)

	if items := listExpenses(t, ts, "?limit=10"); len(items) != 4 {
		t.Fatalf("all: want 4, got %d", len(items))
	}
	if items := listExpenses(t, ts, "?limit=10"); items[0].ID != bobHousing.ID {
		t.Fatalf("list must be sorted by created_at desc, first=%d want %d", items[0].ID, bobHousing.ID)
	}

	if items := listExpenses(t, ts, "?user_id=101&limit=10"); len(items) != 2 || items[0].ID != aliceTransport.ID || items[1].ID != aliceFood.ID {
		t.Fatalf("user filter: %+v", items)
	}
	if items := listExpenses(t, ts, "?date_from=2026-10-15&date_to=2026-10-19&limit=10"); len(items) != 2 {
		t.Fatalf("date range filter: %+v", items)
	}
	if items := listExpenses(t, ts, "?category=food&limit=10"); len(items) != 2 || items[0].ID != bobFood.ID || items[1].ID != aliceFood.ID {
		t.Fatalf("category filter: %+v", items)
	}
	if items := listExpenses(t, ts, "?category=жилье&limit=10"); len(items) != 1 || items[0].ID != bobHousing.ID {
		t.Fatalf("е/ё filter: %+v", items)
	}
	if items := listExpenses(t, ts, "?category=жильё&limit=10"); len(items) != 1 || items[0].ID != bobHousing.ID {
		t.Fatalf("ё filter: %+v", items)
	}
	if items := listExpenses(t, ts, "?user_id=102&category=еда&date_from=2026-10-01&date_to=2026-10-19&limit=10"); len(items) != 1 || items[0].ID != bobFood.ID {
		t.Fatalf("combined filter: %+v", items)
	}
}

func TestExpensesLimit(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")

	created := make([]expenseResponse, 0, 5)
	for i := 0; i < 5; i++ {
		created = append(created, createExpense(t, ts, fmt.Sprintf(
			`{"user_id":101,"amount_rub":"%d","category":"еда","spent_at":"2026-10-1%d"}`, i+1, i+1)))
	}

	if items := listExpenses(t, ts, "?limit=3"); len(items) != 3 || items[0].ID != created[4].ID {
		t.Fatalf("limit=3: %+v", items)
	}
	// no explicit limit: the configured server limit (2) applies, not the full set
	if items := listExpenses(t, ts, ""); len(items) != 2 || items[0].ID != created[4].ID {
		t.Fatalf("config limit: %+v", items)
	}

	status, data := doRequest(t, ts, http.MethodGet, "/expenses?limit=0", "")
	if status != http.StatusBadRequest {
		t.Fatalf("limit=0: status %d: %s", status, data)
	}
	if e := decodeError(t, status, data); e.Error.Code != "bad_request" {
		t.Fatalf("limit=0: want bad_request, got %q", e.Error.Code)
	}
}

func TestCreateValidationErrors(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")

	cases := []struct {
		name string
		body string
		want string
	}{
		{"zero amount", `{"user_id":101,"amount_rub":"0","category":"еда","spent_at":"2026-10-19"}`, "invalid_amount"},
		{"three decimals", `{"user_id":101,"amount_rub":"350.505","category":"еда","spent_at":"2026-10-19"}`, "invalid_amount"},
		{"unknown category", `{"user_id":101,"amount_rub":"100","category":"кафе","spent_at":"2026-10-19"}`, "invalid_category"},
		{"missing fields", `{}`, "bad_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, data := doRequest(t, ts, http.MethodPost, "/expenses", tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status %d: %s", status, data)
			}
			if e := decodeError(t, status, data); e.Error.Code != tc.want {
				t.Fatalf("want %q, got %q (%s)", tc.want, e.Error.Code, data)
			}
		})
	}

	status, data := doRequest(t, ts, http.MethodGet, "/expenses?date_from=19.10.2026", "")
	if status != http.StatusBadRequest {
		t.Fatalf("bad query date: status %d: %s", status, data)
	}
	if e := decodeError(t, status, data); e.Error.Code != "bad_request" {
		t.Fatalf("bad query date: want bad_request, got %q", e.Error.Code)
	}
}

func TestUnknownExpenseIDNotFound(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")

	for _, tc := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"amount_rub":"100"}`},
		{http.MethodDelete, ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			status, data := doRequest(t, ts, tc.method, "/expenses/424242", tc.body)
			if status != http.StatusNotFound {
				t.Fatalf("status %d: %s", status, data)
			}
			if e := decodeError(t, status, data); e.Error.Code != "not_found" {
				t.Fatalf("want not_found, got %q", e.Error.Code)
			}
		})
	}
}

func TestUsersList(t *testing.T) {
	ts, pool := newTestEnv(t)
	seedUser(t, pool, aliceID, "alice", "Алиса")
	seedUser(t, pool, bobID, "", "Борис")

	status, data := doRequest(t, ts, http.MethodGet, "/users", "")
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, data)
	}
	var l struct {
		Items []struct {
			ID        int64   `json:"id"`
			Username  *string `json:"username"`
			FirstName string  `json:"first_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(l.Items) != 2 {
		t.Fatalf("want 2 users, got %d", len(l.Items))
	}
	if l.Items[0].ID != aliceID || l.Items[0].Username == nil || *l.Items[0].Username != "alice" {
		t.Fatalf("alice mismatch: %+v", l.Items[0])
	}
	if l.Items[1].ID != bobID || l.Items[1].Username != nil {
		t.Fatalf("bob must have null username: %+v", l.Items[1])
	}
}

func TestInternalErrorsServe500WithCodeInternal(t *testing.T) {
	ts, pool := newTestEnv(t)

	// deterministic way to fail DB-backed queries without mocking
	pool.Close()

	status, data := doRequest(t, ts, http.MethodGet, "/expenses", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("GET /expenses status %d: %s", status, data)
	}
	var e errorBody
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Error.Code != "internal" {
		t.Fatalf("GET /expenses code %q, want internal", e.Error.Code)
	}

	status, data = doRequest(t, ts, http.MethodGet, "/expenses/42", "")
	if status != http.StatusInternalServerError {
		t.Fatalf("GET /expenses/42 status %d: %s", status, data)
	}
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Error.Code != "internal" {
		t.Fatalf("GET /expenses/42 code %q, want internal", e.Error.Code)
	}
}
