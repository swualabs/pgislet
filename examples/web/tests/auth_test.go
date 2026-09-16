package webtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/swualabs/pgislet"
	"github.com/swualabs/pgislet/examples/web/app"
)

type fixture struct {
	store   *app.Store
	manager *pgislet.Manager
	server  *httptest.Server
	config  app.Config
	handler atomic.Pointer[app.Server]
}

func setup(t *testing.T) *fixture {
	t.Helper()

	if appDSN == "" {
		t.Skip("integration disabled by PGISLET_UNIT_ONLY")
	}

	gin.SetMode(gin.ReleaseMode)
	ctx := context.Background()
	store, err := app.OpenStore(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.DB.Close() })

	if err := store.CheckIsolation(ctx, appDSN); err == nil {
		t.Fatal("same physical database accepted")
	}

	if err := store.CheckIsolation(ctx, isletDSN); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal("migrations were not idempotent:", err)
	}

	manager, err := pgislet.New(ctx, pgislet.Config{DSN: isletDSN, StatementTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(manager.Close)
	f := &fixture{store: store, manager: manager}
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.handler.Load().ServeHTTP(w, r) }))
	f.config = app.Config{AppDSN: appDSN, IsletDSN: isletDSN, Origin: "http://" + f.server.Listener.Addr().String(), SessionTTL: time.Hour, MaxConcurrent: 4, MaxAccounts: 100, AuthRate: 1000, Signup: true}
	f.restart(t)
	f.server.Start()
	t.Cleanup(f.server.Close)
	t.Cleanup(func() {
		var accounts []app.Account

		if err := store.DB.NewSelect().Model(&accounts).Scan(ctx); err != nil {
			t.Error(err)
			return
		}

		for _, account := range accounts {
			if account.WorkspaceID != "" {
				h, err := manager.Open(ctx, account.WorkspaceID)
				if err == nil {
					err = manager.Delete(ctx, h)
				}

				if err != nil {
					t.Error(err)
				}
			}
		}

		if _, err := store.DB.ExecContext(ctx, `TRUNCATE accounts, sessions, auth_limits CASCADE`); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *fixture) restart(t *testing.T) {
	t.Helper()
	server, err := app.New(f.config, f.store, f.manager, fstest.MapFS{"index.html": {Data: []byte("playground")}})
	if err != nil {
		t.Fatal(err)
	}

	f.handler.Store(server)
}

func client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 20 * time.Second}
}

func request(t *testing.T, f *fixture, c *http.Client, method, path string, body any, status int) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, f.server.URL+path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Pgislet-Request", "playground")
	response, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)

	if response.StatusCode != status {
		t.Fatalf("%s %s: expected %d got %d: %s", method, path, status, response.StatusCode, raw)
	}

	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}

func register(t *testing.T, f *fixture, c *http.Client, email string) {
	t.Helper()
	result := request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": email, "name": "Test User", "password": "a strong test password"}, 201)
	encoded, _ := json.Marshal(result)

	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "token") {
		t.Fatal("registration response exposed credentials")
	}
}

func TestAuthenticationAndIsolation(t *testing.T) {
	f := setup(t)
	alice, bob := client(), client()
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT 1"}, 401)
	register(t, f, alice, "alice@example.com")
	register(t, f, bob, "bob@example.com")
	a := request(t, f, alice, "POST", "/api/session", nil, 200)
	b := request(t, f, bob, "POST", "/api/session", nil, 200)

	if a["ID"] == b["ID"] {
		t.Fatal("accounts shared a workspace")
	}

	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "CREATE TABLE private_notes(value text)"}, 200)
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "INSERT INTO private_notes VALUES('alice-only')"}, 200)
	request(t, f, bob, "POST", "/api/query", map[string]string{"sql": "SELECT * FROM private_notes"}, 400)
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT * FROM public.accounts"}, 400)
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT set_config('role','postgres',false)"}, 400)
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT 1", "id": fmt.Sprint(b["ID"])}, 400)

	f.restart(t)
	persisted := request(t, f, alice, "POST", "/api/session", nil, 200)

	if persisted["ID"] != a["ID"] {
		t.Fatal("workspace did not survive application reconstruction")
	}

	request(t, f, alice, "POST", "/api/auth/logout", nil, 200)
	request(t, f, alice, "GET", "/api/auth/me", nil, 401)
	request(t, f, alice, "POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "wrong password"}, 401)
	request(t, f, alice, "POST", "/api/auth/login", map[string]string{"email": " ALICE@EXAMPLE.COM ", "password": "a strong test password"}, 200)
	result := request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT * FROM private_notes"}, 200)
	raw, _ := json.Marshal(result)

	if !strings.Contains(string(raw), "alice-only") {
		t.Fatal("workspace data was lost on logout")
	}

	request(t, f, alice, "POST", "/api/seed", nil, 200)
	request(t, f, alice, "POST", "/api/query", map[string]string{"sql": "SELECT * FROM private_notes"}, 400)
	request(t, f, bob, "GET", "/api/schema", nil, 200)
}

func TestPasswordAndSessions(t *testing.T) {
	f := setup(t)
	first, second := client(), client()
	register(t, f, first, "sessions@example.com")
	request(t, f, second, "POST", "/api/auth/login", map[string]string{"email": "sessions@example.com", "password": "a strong test password"}, 200)
	request(t, f, first, "POST", "/api/auth/password", map[string]string{"currentPassword": "wrong", "newPassword": "a changed strong password"}, 401)
	request(t, f, first, "POST", "/api/auth/password", map[string]string{"currentPassword": "a strong test password", "newPassword": "a changed strong password"}, 200)
	request(t, f, second, "GET", "/api/auth/me", nil, 401)
	request(t, f, second, "POST", "/api/auth/login", map[string]string{"email": "sessions@example.com", "password": "a strong test password"}, 401)
	request(t, f, second, "POST", "/api/auth/login", map[string]string{"email": "sessions@example.com", "password": "a changed strong password"}, 200)

	if _, err := f.store.DB.ExecContext(context.Background(), `UPDATE sessions SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}

	request(t, f, second, "GET", "/api/auth/me", nil, 401)

	if err := f.store.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRequestProtectionAndSessionStorage(t *testing.T) {
	f := setup(t)
	c := client()
	register(t, f, c, "boundary@example.com")
	parsed, _ := http.NewRequest("GET", f.server.URL, nil)
	cookies := c.Jar.Cookies(parsed.URL)

	if len(cookies) != 1 {
		t.Fatal("session cookie not issued")
	}

	var stored string

	if err := f.store.DB.NewRaw(`SELECT token_hash FROM sessions s JOIN accounts a ON a.id=s.account_id WHERE a.email=?`, "boundary@example.com").Scan(context.Background(), &stored); err != nil {
		t.Fatal(err)
	}

	if len(stored) != 64 || stored == cookies[0].Value {
		t.Fatal("database stores a usable session token")
	}

	for _, origin := range []string{"https://evil.example", "null"} {
		req, _ := http.NewRequest("POST", f.server.URL+"/api/auth/logout", strings.NewReader(`{}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Pgislet-Request", "playground")
		req.Header.Set("Content-Type", "application/json")
		response, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = response.Body.Close()

		if response.StatusCode != 403 {
			t.Fatal("foreign origin accepted")
		}
	}

	request(t, f, c, "GET", "/api/auth/me", nil, 200)
	saved := cookies[0]
	request(t, f, c, "POST", "/api/auth/logout", nil, 200)
	c.Jar.SetCookies(parsed.URL, []*http.Cookie{saved})
	request(t, f, c, "GET", "/api/auth/me", nil, 401)

	if _, err := f.store.DB.ExecContext(context.Background(), `DELETE FROM auth_limits`); err != nil {
		t.Fatal(err)
	}

	f.config.AuthRate = 1
	f.restart(t)
	request(t, f, c, "POST", "/api/auth/login", map[string]string{"email": "unknown@example.com", "password": "wrong"}, 401)
	request(t, f, c, "POST", "/api/auth/login", map[string]string{"email": "unknown@example.com", "password": "wrong"}, 429)
}

func TestRegistrationValidationAndCapacity(t *testing.T) {
	f := setup(t)
	c := client()
	request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": "invalid", "name": "Test", "password": "a strong test password"}, 400)
	request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": "valid@example.com", "name": "Test", "password": "short"}, 400)
	register(t, f, c, "duplicate@example.com")
	request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": "DUPLICATE@EXAMPLE.COM", "name": "Test", "password": "a strong test password"}, 409)
	f.config.Signup = false
	f.restart(t)
	request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": "closed@example.com", "name": "Test", "password": "a strong test password"}, 403)
	f.config.Signup, f.config.MaxAccounts = true, 1
	f.restart(t)
	request(t, f, c, "POST", "/api/auth/register", map[string]string{"email": "capacity@example.com", "name": "Test", "password": "a strong test password"}, 503)
}

func TestWorkspaceConcurrentProvisioning(t *testing.T) {
	f := setup(t)
	c := client()
	register(t, f, c, "concurrent@example.com")
	start := make(chan struct{})
	outcomes := make(chan int, 8)

	for range 8 {
		go func() {
			<-start
			req, _ := http.NewRequest("POST", f.server.URL+"/api/session", strings.NewReader(`{}`))
			req.Header.Set("X-Pgislet-Request", "playground")
			req.Header.Set("Content-Type", "application/json")
			response, err := c.Do(req)
			if err != nil {
				outcomes <- 0
				return
			}

			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			outcomes <- response.StatusCode
		}()
	}

	close(start)
	successes := 0

	for range 8 {
		status := <-outcomes

		if status == 200 {
			successes++
		} else if status != 409 && status != 503 {
			t.Fatalf("unexpected concurrent provisioning response: %d", status)
		}
	}

	if successes == 0 {
		t.Fatal("no provisioning request completed")
	}

	first := request(t, f, c, "POST", "/api/session", nil, 200)
	second := request(t, f, c, "POST", "/api/session", nil, 200)

	if first["ID"] != second["ID"] || isletCount(t) != 1 {
		t.Fatal("concurrent requests created duplicate or orphan workspaces")
	}
}

func TestWorkspaceProvisioningCompensation(t *testing.T) {
	f := setup(t)
	c := client()
	register(t, f, c, "compensation@example.com")
	ctx := context.Background()

	if _, err := f.store.DB.ExecContext(ctx, `ALTER TABLE accounts ADD CONSTRAINT reject_workspace CHECK(workspace_id='')`); err != nil {
		t.Fatal(err)
	}

	defer func() {
		_, err := f.store.DB.ExecContext(ctx, `ALTER TABLE accounts DROP CONSTRAINT reject_workspace`)
		if err != nil {
			t.Error(err)
		}
	}()

	request(t, f, c, "POST", "/api/session", nil, 500)

	if isletCount(t) != 0 {
		t.Fatal("failed application mapping left an orphan workspace")
	}

	request(t, f, c, "GET", "/api/auth/me", nil, 200)
}

func isletCount(t *testing.T) int {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), isletDSN)
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close(context.Background())
	var count int

	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM pgislet_internal.islets`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	return count
}
