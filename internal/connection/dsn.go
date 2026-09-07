package connection

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Config interface {
	PostgreSQLParams() Params
}

type Params struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
}

func (p Params) PostgreSQLParams() Params {
	return p
}

func BuildDSN(config Config) (string, error) {
	if config == nil {
		return "", errors.New("PostgreSQL connection configuration is required")
	}

	p := config.PostgreSQLParams()

	if p.Host == "" || p.Database == "" || p.Username == "" {
		return "", errors.New("PostgreSQL host, database, and username are required")
	}

	for _, value := range []string{p.Host, p.Database, p.Username, p.Password} {
		if strings.ContainsRune(value, 0) {
			return "", errors.New("PostgreSQL connection parameters cannot contain NUL")
		}
	}

	if p.Port == 0 {
		p.Port = 5432
	}

	if p.Port < 1 || p.Port > 65535 {
		return "", errors.New("PostgreSQL port must be between 1 and 65535")
	}

	if p.SSLMode == "" {
		p.SSLMode = "verify-full"
	}

	switch p.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return "", errors.New("invalid PostgreSQL SSL mode")
	}

	query := url.Values{"sslmode": {p.SSLMode}}
	host := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))

	if strings.HasPrefix(p.Host, "/") {
		host = ""
		query.Set("host", p.Host)
		query.Set("port", strconv.Itoa(p.Port))
	} else if strings.ContainsAny(p.Host, "/?#@[] \t\r\n") || (strings.Contains(p.Host, ":") && net.ParseIP(p.Host) == nil) {
		return "", errors.New("PostgreSQL host must be a hostname, unbracketed IP, or absolute socket directory")
	}

	u := url.URL{
		Scheme:   "postgresql",
		User:     url.UserPassword(p.Username, p.Password),
		Host:     host,
		Path:     "/" + p.Database,
		RawPath:  "/" + url.PathEscape(p.Database),
		RawQuery: query.Encode(),
	}

	return u.String(), nil
}
