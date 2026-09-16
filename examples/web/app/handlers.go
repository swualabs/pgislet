package app

import (
	"net/http"
	"strings"

	"github.com/swualabs/pgislet"
)

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SQL string `json:"sql"`
	}

	if !decode(w, r, &request) {
		return
	}

	if strings.TrimSpace(request.SQL) == "" {
		writeError(w, http.StatusBadRequest, "request", "Enter SQL to execute.")
		return
	}

	h, ok := s.current(w, r)

	if !ok {
		return
	}

	result, err := s.manager.Execute(r.Context(), h, request.SQL)
	if err != nil {
		report(w, err, &result)
		return
	}

	respond(w, http.StatusOK, map[string]any{"result": present(result)})
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, false)
}

func (s *Server) reseed(w http.ResponseWriter, r *http.Request) {
	s.change(w, r, true)
}

func (s *Server) change(w http.ResponseWriter, r *http.Request, reseed bool) {
	h, ok := s.current(w, r)

	if !ok {
		return
	}

	var next pgislet.Islet

	var err error

	if reseed {
		next, err = s.manager.Reinitialize(r.Context(), h, seed)
	} else {
		next, err = s.manager.Reset(r.Context(), h)
	}

	if err != nil {
		report(w, err, nil)
		return
	}

	respond(w, http.StatusOK, next)
}

type table struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Columns []column `json:"columns"`
}

type column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	h, ok := s.current(w, r)

	if !ok {
		return
	}

	result, err := s.manager.Execute(r.Context(), h, schemaQuery)
	if err != nil {
		report(w, err, nil)
		return
	}

	tables := []table{}

	for _, row := range result.Rows {
		name := row[0].(string)

		if len(tables) == 0 || tables[len(tables)-1].Name != name {
			tables = append(tables, table{Name: name, Kind: row[1].(string), Columns: []column{}})
		}

		if row[2] == nil {
			continue
		}

		current := &tables[len(tables)-1]
		current.Columns = append(current.Columns, column{Name: row[2].(string), Type: row[3].(string)})
	}

	respond(w, http.StatusOK, map[string]any{"tables": tables, "islet": h})
}

func present(result pgislet.Result) map[string]any {
	return map[string]any{
		"columns":      result.Columns,
		"rows":         result.Rows,
		"commandTag":   result.CommandTag,
		"rowsAffected": result.RowsAffected,
		"durationMs":   float64(result.Duration.Microseconds()) / 1000,
		"truncated":    result.Truncated,
	}
}
