package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRequestBoundary(t *testing.T) {
	handler := New(nil, fstest.MapFS{"index.html": {Data: []byte("playground")}})
	cases := []struct {
		name, method, path, host, origin, header string
		status                                   int
	}{
		{name: "static", method: "GET", path: "/", host: "localhost:8080", status: 200},
		{name: "foreign host", method: "GET", path: "/", host: "evil.example", status: 403},
		{name: "CSRF", method: "POST", path: "/api/session", host: "localhost:8080", status: 403},
		{name: "foreign origin", method: "POST", path: "/api/session", host: "localhost:8080", origin: "https://evil.example", header: "playground", status: 403},
		{name: "missing session", method: "GET", path: "/api/schema", host: "localhost:8080", status: 401},
		{name: "invalid session", method: "GET", path: "/api/schema", host: "localhost:8080", status: 401},
		{name: "unknown API", method: "POST", path: "/api/absent", host: "localhost:8080", header: "playground", status: 404},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://"+test.host+test.path, nil)
			request.Header.Set("X-Pgislet-Request", test.header)
			request.Header.Set("Origin", test.origin)

			if test.name == "invalid session" {
				request.AddCookie(&http.Cookie{Name: cookieName, Value: "forged"})
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}

			if test.name == "static" && (!strings.Contains(response.Body.String(), "playground") || response.Header().Get("Content-Security-Policy") == "") {
				t.Fatal("missing static content or security headers")
			}
		})
	}
}

func TestJSONInput(t *testing.T) {
	cases := []struct {
		name, body, contentType string
		ok                      bool
	}{
		{name: "valid", body: `{"sql":"SELECT 1"}`, contentType: "application/json", ok: true},
		{name: "content type", body: `{}`, contentType: "text/plain"},
		{name: "unknown field", body: `{"id":"another-islet"}`, contentType: "application/json"},
		{name: "trailing JSON", body: `{"sql":"a"} {}`, contentType: "application/json"},
		{name: "malformed", body: `{`, contentType: "application/json"},
		{name: "oversized", body: `{"sql":"` + strings.Repeat("x", 65<<10) + `"}`, contentType: "application/json"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var value struct {
				SQL string `json:"sql"`
			}
			request := httptest.NewRequest("POST", "/api/query", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)

			if got := decode(httptest.NewRecorder(), request, &value); got != test.ok {
				t.Fatalf("decoded=%v", got)
			}
		})
	}
}
