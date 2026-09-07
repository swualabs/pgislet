package connection

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type applicationConfig struct {
	params Params
}

func (c applicationConfig) PostgreSQLParams() Params {
	return c.params
}

func TestBuildDSNRoundTrip(t *testing.T) {
	for _, host := range []string{"localhost", "::1", "/tmp/postgres socket"} {
		t.Run(host, func(t *testing.T) {
			params := Params{Host: host, Database: "db/name ?#%\u00e9", Username: "user:@ /%", Password: "secret:'@/?#%\\", SSLMode: "disable"}
			dsn, err := BuildDSN(applicationConfig{params: params})
			if err != nil {
				t.Fatal(err)
			}

			parsed, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatal(err)
			}

			if parsed.Host != host || parsed.Port != 5432 || parsed.Database != params.Database || parsed.User != params.Username || parsed.Password != params.Password {
				t.Fatal("connection parameters changed during DSN conversion")
			}
		})
	}
}

func TestBuildDSNDefaults(t *testing.T) {
	dsn, err := BuildDSN(Params{Host: "localhost", Database: "example", Username: "postgres", Port: 5544})
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Port != 5544 || parsed.TLSConfig == nil || parsed.TLSConfig.InsecureSkipVerify || parsed.TLSConfig.ServerName != "localhost" || len(parsed.Fallbacks) != 0 {
		t.Fatal("explicit port and default verified TLS were not preserved")
	}
}

func TestBuildDSNRejectsInvalidParams(t *testing.T) {
	valid := Params{Host: "localhost", Database: "example", Username: "postgres", Password: "private-secret"}
	cases := []Config{nil, Params{}}

	for _, change := range []func(*Params){
		func(p *Params) { p.Port = -1 },
		func(p *Params) { p.Port = 65536 },
		func(p *Params) { p.SSLMode = "invalid" },
		func(p *Params) { p.Password = "private-secret\x00" },
		func(p *Params) { p.Host = "localhost:5432" },
		func(p *Params) { p.Host = "localhost?x=y" },
	} {
		p := valid
		change(&p)
		cases = append(cases, p)
	}

	for _, config := range cases {
		dsn, err := BuildDSN(config)
		if err == nil || dsn != "" {
			t.Fatal("invalid configuration accepted")
		}

		if strings.Contains(err.Error(), "private-secret") {
			t.Fatal("error exposed password")
		}
	}
}
