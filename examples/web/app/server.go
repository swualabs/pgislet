package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/swualabs/pgislet"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	manager   *pgislet.Manager
	store     *Store
	config    Config
	handler   http.Handler
	slots     chan struct{}
	passwords chan struct{}
	dummyHash []byte
}

func New(cfg Config, store *Store, manager *pgislet.Manager, assets fs.FS) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte(randomToken()), 12)
	if err != nil {
		return nil, err
	}

	s := &Server{manager: manager, store: store, config: cfg, slots: make(chan struct{}, cfg.MaxConcurrent), passwords: make(chan struct{}, 2), dummyHash: dummy}
	router := gin.New()

	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	router.Use(func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				slog.Error("request panic")
				writeError(c.Writer, 500, "internal", "Unable to complete the request.")
				c.Abort()
			}
		}()

		c.Next()
	})
	router.Use(s.boundary)
	router.GET("/healthz", func(c *gin.Context) {
		c.Status(200)
	})
	router.GET("/readyz", gin.WrapF(s.ready))
	router.GET("/api/config", func(c *gin.Context) {
		c.JSON(200, gin.H{"signup": cfg.Signup})
	})
	auth := router.Group("/api/auth")
	auth.POST("/register", s.authThrottle, gin.WrapF(s.register))
	auth.POST("/login", s.authThrottle, gin.WrapF(s.login))
	auth.GET("/me", gin.WrapF(s.me))
	auth.POST("/logout", gin.WrapF(s.logout))
	auth.POST("/password", s.authThrottle, gin.WrapF(s.changePassword))
	router.POST("/api/session", s.capacity, gin.WrapF(s.start))
	router.POST("/api/query", s.capacity, gin.WrapF(s.query))
	router.POST("/api/reset", s.capacity, gin.WrapF(s.reset))
	router.POST("/api/seed", s.capacity, gin.WrapF(s.reseed))
	router.GET("/api/schema", s.capacity, gin.WrapF(s.schema))
	files := http.FileServerFS(assets)
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			writeError(c.Writer, 404, "not_found", "API endpoint not found.")
			return
		}

		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Header("Allow", "GET, HEAD")
			c.Status(405)
			return
		}

		files.ServeHTTP(c.Writer, c.Request)
	})
	s.handler = router
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) boundary(c *gin.Context) {
	start := time.Now()
	origin, _ := url.Parse(s.config.Origin)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	c.Header("Cache-Control", "no-store")

	if s.config.SecureCookies {
		c.Header("Strict-Transport-Security", "max-age=31536000")
	}

	if c.Request.URL.Path != "/healthz" && c.Request.URL.Path != "/readyz" && c.Request.Host != origin.Host {
		writeError(c.Writer, 403, "origin", "Unexpected request host.")
		c.Abort()
		return
	}

	if strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		supplied := c.GetHeader("Origin")

		if (supplied != "" && supplied != s.config.Origin) || c.GetHeader("Sec-Fetch-Site") == "cross-site" || c.GetHeader("X-Pgislet-Request") != "playground" {
			writeError(c.Writer, 403, "origin", "Send requests from the same web page.")
			c.Abort()
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()

	c.Request = c.Request.WithContext(ctx)
	c.Next()
	slog.Info("http request", "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(start))
}

func (s *Server) capacity(c *gin.Context) {
	select {
	case s.slots <- struct{}{}:
		defer func() {
			<-s.slots
		}()
		c.Next()
	default:
		c.Header("Retry-After", "1")
		writeError(c.Writer, 503, "capacity", "The server is busy. Try again shortly.")
		c.Abort()
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.store.DB.PingContext(ctx); err != nil {
		writeError(w, 503, "unavailable", "Application database unavailable.")
		return
	}

	conn, err := pgx.Connect(ctx, s.config.IsletDSN)
	if err != nil {
		writeError(w, 503, "unavailable", "Workspace database unavailable.")
		return
	}

	_ = conn.Close(ctx)
	respond(w, 200, map[string]string{"status": "ready"})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeError(w, 415, "request", "A JSON request is required.")
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(value); err != nil {
		writeError(w, 400, "request", "The request is invalid or exceeds 64 KiB.")
		return false
	}

	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, 400, "request", "Send only one JSON value per request.")
		return false
	}

	return true
}
