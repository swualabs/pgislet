package app

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
)

type apiError struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Position int32  `json:"position,omitempty"`
}

func writeError(w http.ResponseWriter, status int, kind, message string) {
	respond(w, status, map[string]any{"error": apiError{Kind: kind, Message: message}})
}

func report(w http.ResponseWriter, err error, result *pgislet.Result) {
	status := http.StatusInternalServerError
	problem := apiError{Kind: "internal", Message: "Unable to complete the operation. Check the server logs and connection."}

	switch {
	case errors.Is(err, pgislet.ErrOutcomeUnknown):
		problem = apiError{Kind: "unknown", Message: "The commit outcome is unknown. Check the data before retrying."}
	case errors.Is(err, pgislet.ErrBusy), errors.Is(err, pgislet.ErrStaleGeneration):
		status = http.StatusConflict
		problem = apiError{Kind: "busy", Message: "Another operation is running or the workspace has changed. Try again shortly."}
	case errors.Is(err, pgislet.ErrFailed), errors.Is(err, pgislet.ErrUnavailable):
		status = http.StatusConflict
		problem = apiError{Kind: "unavailable", Message: "The workspace is unavailable. Restore the sample data or clear the workspace."}
	case errors.Is(err, pgislet.ErrNotFound):
		status = http.StatusConflict
		problem = apiError{Kind: "workspace", Message: "Workspace not found. Contact the operator to restore its mapping."}
	case errors.Is(err, pgislet.ErrTimeout):
		status = http.StatusRequestTimeout
		problem = apiError{Kind: "timeout", Message: "Execution timed out. The operation was not committed."}
	case errors.Is(err, pgislet.ErrResultLimit):
		status = http.StatusUnprocessableEntity
		problem = apiError{Kind: "limit", Message: "The result limit was exceeded and the entire operation was rolled back. Adjust LIMIT or narrow the query."}
	case errors.Is(err, pgislet.ErrSQLTooLarge):
		status = http.StatusRequestEntityTooLarge
		problem = apiError{Kind: "limit", Message: "SQL input is too large."}
	case errors.Is(err, pgislet.ErrPolicy):
		status = http.StatusBadRequest
		problem = apiError{Kind: "policy", Message: "SQL is not allowed. Run one statement without role, session, or schema management commands."}
	case errors.Is(err, pgislet.ErrRuntimeConnection):
		status = http.StatusServiceUnavailable
		problem = apiError{Kind: "connection", Message: "Unable to connect to PostgreSQL."}
	case errors.Is(err, pgislet.ErrQuery):
		status = http.StatusBadRequest
		problem.Kind = "query"
		if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
			problem.Message = pe.Message
			problem.Code = pe.Code
			problem.Detail = pe.Detail
			problem.Hint = pe.Hint
			problem.Position = pe.Position
		}
	}

	body := map[string]any{"error": problem}

	if result != nil {
		body["result"] = present(*result)
	}

	respond(w, status, body)
}
