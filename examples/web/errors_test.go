package main

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

func TestErrorResponses(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		kind   string
	}{
		{pgislet.ErrBusy, 409, "busy"}, {pgislet.ErrStaleGeneration, 409, "busy"},
		{pgislet.ErrFailed, 409, "unavailable"}, {pgislet.ErrNotFound, 401, "session"},
		{pgislet.ErrTimeout, 408, "timeout"}, {pgislet.ErrResultLimit, 422, "limit"},
		{pgislet.ErrSQLTooLarge, 413, "limit"}, {pgislet.ErrPolicy, 400, "policy"},
		{pgislet.ErrRuntimeConnection, 503, "connection"}, {pgislet.ErrOutcomeUnknown, 500, "unknown"},
		{errors.New("password=must-not-leak"), 500, "internal"},
	} {
		response := httptest.NewRecorder()
		report(response, test.err, &pgislet.Result{})

		if response.Code != test.status || !strings.Contains(response.Body.String(), `"kind":"`+test.kind+`"`) || strings.Contains(response.Body.String(), "must-not-leak") {
			t.Fatal(response.Body.String())
		}
	}

	pe := &pgconn.PgError{Code: "42703", Message: "unknown column", Position: 8, Hint: "check name"}
	response := httptest.NewRecorder()
	report(response, &pgislet.Error{Kind: pgislet.ErrQuery, Cause: pe}, nil)

	if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"42703"`) {
		t.Fatal(response.Body.String())
	}
}
