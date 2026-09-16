package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionCookie(t *testing.T) {
	cfg := testConfig()
	cfg.SecureCookies = true
	server := &Server{config: cfg}
	response := httptest.NewRecorder()
	server.setCookie(response, "opaque-token")
	cookies := response.Result().Cookies()

	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}

	cookie := cookies[0]

	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 3600 {
		t.Fatal("session cookie protections missing")
	}
}
