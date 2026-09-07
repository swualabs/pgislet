package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

func TestSessionExpirationAndCapacity(t *testing.T) {
	handler := New(nil, fstest.MapFS{})
	handler.sessions["expired"] = session{id: "old", used: time.Now().Add(-sessionTTL - time.Second)}
	request := httptest.NewRequest("GET", "http://localhost/api/schema", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: "expired"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}

	for i := range maxSessions {
		handler.sessions[fmt.Sprint(i)] = session{used: time.Now()}
	}

	request = httptest.NewRequest("POST", "http://localhost/api/session", nil)
	request.Header.Set("X-Pgislet-Request", "playground")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code)
	}
}
