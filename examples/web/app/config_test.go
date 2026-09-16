package app

import (
	"testing"
	"time"
)

func testConfig() Config {
	return Config{AppDSN: "postgres://localhost/app", IsletDSN: "postgres://localhost/islets", Origin: "http://localhost:8080", SessionTTL: time.Hour, MaxConcurrent: 2, MaxAccounts: 100, AuthRate: 10, Signup: true}
}

func TestConfigValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"shared database", func(c *Config) { c.IsletDSN = c.AppDSN }},
		{"missing application database", func(c *Config) { c.AppDSN = "" }},
		{"insecure public origin", func(c *Config) { c.Origin = "http://example.com" }},
		{"origin path", func(c *Config) { c.Origin += "/login" }},
		{"insecure cookies", func(c *Config) { c.Origin = "https://example.com" }},
		{"capacity", func(c *Config) { c.MaxConcurrent = 0 }},
		{"session duration", func(c *Config) { c.SessionTTL = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig()
			test.change(&cfg)

			if err := cfg.Validate(); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}

	cfg := testConfig()
	cfg.Origin, cfg.SecureCookies = "https://example.com", true

	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
