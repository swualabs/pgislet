package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/swualabs/pgislet"
)

type Server struct {
	manager  *pgislet.Manager
	handler  http.Handler
	mu       sync.Mutex
	sessions map[string]session
	pending  int
}

func New(manager *pgislet.Manager, assets fs.FS) *Server {
	s := &Server{manager: manager, sessions: map[string]session{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session", s.start)
	mux.HandleFunc("DELETE /api/session", s.end)
	mux.HandleFunc("POST /api/query", s.query)
	mux.HandleFunc("POST /api/reset", s.reset)
	mux.HandleFunc("POST /api/seed", s.reseed)
	mux.HandleFunc("GET /api/schema", s.schema)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "API endpoint not found.")
	})
	files := http.FileServerFS(assets)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		files.ServeHTTP(w, r)
	})
	s.handler = mux
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host

	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}

	ip := net.ParseIP(host)

	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		writeError(w, http.StatusForbidden, "origin", "Run this example on localhost.")
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")

	if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			scheme := "http"

			if r.TLS != nil {
				scheme = "https"
			}

			if err != nil || parsed.Host != r.Host || parsed.Scheme != scheme {
				writeError(w, http.StatusForbidden, "origin", "Requests from other sites are not allowed.")
				return
			}
		}

		if r.Header.Get("X-Pgislet-Request") != "playground" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "origin", "Send requests from the same web page.")
			return
		}
	}

	s.handler.ServeHTTP(w, r)
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "request", "A JSON request is required.")
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "request", "The request is invalid or exceeds 64 KiB.")
		return false
	}

	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request", "Send only one JSON value per request.")
		return false
	}

	return true
}
