package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	AppDSN         string
	IsletDSN       string
	Origin         string
	Address        string
	Assets         string
	SecureCookies  bool
	Signup         bool
	SessionTTL     time.Duration
	MaxConcurrent  int
	MaxAccounts    int
	AuthRate       int
	TrustedProxies []string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		AppDSN: os.Getenv("APP_DATABASE_URL"), IsletDSN: os.Getenv("PGISLET_DATABASE_URL"),
		Origin: env("APP_ORIGIN", "http://localhost:8080"), Address: env("APP_ADDR", "127.0.0.1:8080"),
		Assets: env("APP_ASSETS", "web/static"), SessionTTL: 24 * time.Hour,
		Signup: true, MaxConcurrent: 8, MaxAccounts: 1000, AuthRate: 20,
	}

	for key, target := range map[string]*int{"APP_MAX_CONCURRENT": &cfg.MaxConcurrent, "APP_MAX_ACCOUNTS": &cfg.MaxAccounts, "APP_AUTH_RATE": &cfg.AuthRate} {
		if value := os.Getenv(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return cfg, fmt.Errorf("invalid %s", key)
			}

			*target = parsed
		}
	}

	if value := os.Getenv("APP_SIGNUP"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, fmt.Errorf("invalid APP_SIGNUP")
		}

		cfg.Signup = parsed
	}

	if value := os.Getenv("APP_TRUSTED_PROXIES"); value != "" {
		cfg.TrustedProxies = strings.Split(value, ",")
	}

	cfg.SecureCookies = strings.HasPrefix(cfg.Origin, "https://")
	return cfg, cfg.Validate()
}

func (cfg Config) Validate() error {
	origin, err := url.Parse(cfg.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "http" && origin.Scheme != "https") {
		return fmt.Errorf("APP_ORIGIN must be an HTTP(S) origin without a path")
	}

	host := origin.Hostname()
	ip := net.ParseIP(host)

	if origin.Scheme == "http" && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("non-local APP_ORIGIN requires HTTPS")
	}

	if cfg.SecureCookies != (origin.Scheme == "https") || cfg.SessionTTL < time.Minute || cfg.SessionTTL > 7*24*time.Hour || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 128 || cfg.MaxAccounts < 1 || cfg.AuthRate < 1 {
		return fmt.Errorf("invalid session, cookie, or capacity settings")
	}

	if cfg.AppDSN == "" || cfg.IsletDSN == "" {
		return fmt.Errorf("APP_DATABASE_URL and PGISLET_DATABASE_URL are required")
	}

	general, err := pgx.ParseConfig(cfg.AppDSN)
	if err != nil {
		return fmt.Errorf("invalid APP_DATABASE_URL")
	}

	islet, err := pgx.ParseConfig(cfg.IsletDSN)
	if err != nil {
		return fmt.Errorf("invalid PGISLET_DATABASE_URL")
	}

	if general.Host == islet.Host && general.Port == islet.Port && general.Database == islet.Database {
		return fmt.Errorf("application and pgislet databases must be separate")
	}

	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
