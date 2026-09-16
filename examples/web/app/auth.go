package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"
)

const cookieName = "pgislet_session"

func (s *Server) authThrottle(c *gin.Context) {
	var count int
	err := s.store.DB.NewRaw(`INSERT INTO auth_limits(key,requests,window_start) VALUES (?,1,now()) ON CONFLICT(key) DO UPDATE SET requests=CASE WHEN auth_limits.window_start < now()-interval '1 minute' THEN 1 ELSE auth_limits.requests+1 END, window_start=CASE WHEN auth_limits.window_start < now()-interval '1 minute' THEN now() ELSE auth_limits.window_start END RETURNING requests`, tokenHash(c.ClientIP())).Scan(c.Request.Context(), &count)
	if err != nil {
		writeError(c.Writer, 503, "unavailable", "Authentication is temporarily unavailable.")
		c.Abort()
		return
	}

	if count > s.config.AuthRate {
		c.Header("Retry-After", "60")
		writeError(c.Writer, 429, "rate_limit", "Too many attempts. Try again in a minute.")
		c.Abort()
		return
	}

	select {
	case s.passwords <- struct{}{}:
		defer func() {
			<-s.passwords
		}()
		c.Next()
	default:
		writeError(c.Writer, 503, "capacity", "Authentication is busy. Try again shortly.")
		c.Abort()
	}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.config.Signup {
		writeError(w, 403, "signup", "Registration is currently closed.")
		return
	}

	var input struct{ Email, Name, Password string }

	if !decode(w, r, &input) {
		return
	}

	email, err := normalizeEmail(input.Email)
	input.Name = strings.TrimSpace(input.Name)

	if err != nil || !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 80 {
		writeError(w, 400, "validation", "Enter a valid email address and a name of 1 to 80 characters.")
		return
	}

	hash, err := hashPassword(input.Password)
	if err != nil {
		writeError(w, 400, "validation", err.Error())
		return
	}

	user := Account{ID: randomToken(), Email: email, Name: input.Name, PasswordHash: hash, CreatedAt: time.Now().UTC()}
	token := randomToken()
	err = s.store.DB.RunInTx(r.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(722031995)`); err != nil {
			return err
		}

		count, err := tx.NewSelect().Model((*Account)(nil)).Count(ctx)
		if err != nil {
			return err
		}

		if count >= s.config.MaxAccounts {
			return errAccountCapacity
		}

		if _, err := tx.NewInsert().Model(&user).Exec(ctx); err != nil {
			return err
		}

		return s.insertSession(ctx, tx, token, user.ID)
	})
	if err != nil {
		var pgerr *pgconn.PgError

		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			writeError(w, 409, "account", "An account with this email already exists.")
		} else if errors.Is(err, errAccountCapacity) {
			writeError(w, 503, "capacity", "Registration capacity has been reached.")
		} else {
			writeError(w, 503, "unavailable", "Unable to create the account. Try signing in before retrying.")
		}

		return
	}

	s.setCookie(w, token)
	respond(w, 201, map[string]any{"user": user})
}

var errAccountCapacity = errors.New("account capacity reached")
var errCredentials = errors.New("invalid credentials")

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct{ Email, Password string }

	if !decode(w, r, &input) {
		return
	}

	email, _ := normalizeEmail(input.Email)
	var user Account

	err := s.store.DB.NewSelect().Model(&user).Where("email = ?", email).Scan(r.Context())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, 503, "unavailable", "Authentication is temporarily unavailable.")
		return
	}

	hash := s.dummyHash

	if err == nil {
		hash = []byte(user.PasswordHash)
	}

	compare := bcrypt.CompareHashAndPassword(hash, []byte(input.Password))

	if compare != nil || user.ID == "" || len(input.Password) > 72 {
		writeError(w, 401, "credentials", "Email or password is incorrect.")
		return
	}

	token := randomToken()
	err = s.store.DB.RunInTx(r.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		var current Account

		if err := tx.NewSelect().Model(&current).Where("id = ?", user.ID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}

		if current.PasswordHash != user.PasswordHash {
			return errCredentials
		}

		if cookie, err := r.Cookie(cookieName); err == nil {
			if _, err := tx.NewDelete().Model((*Session)(nil)).Where("token_hash = ?", tokenHash(cookie.Value)).Exec(ctx); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE account_id=? AND token_hash NOT IN (SELECT token_hash FROM sessions WHERE account_id=? ORDER BY created_at DESC LIMIT 9)`, user.ID, user.ID); err != nil {
			return err
		}

		return s.insertSession(ctx, tx, token, user.ID)
	})
	if err != nil {
		writeError(w, 503, "unavailable", "Unable to sign in. Try again.")
		return
	}

	s.setCookie(w, token)
	respond(w, 200, map[string]any{"user": user})
}

func (s *Server) insertSession(ctx context.Context, tx bun.Tx, token, userID string) error {
	session := Session{TokenHash: tokenHash(token), AccountID: userID, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(s.config.SessionTTL).UTC()}
	_, err := tx.NewInsert().Model(&session).Exec(ctx)
	return err
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: int(s.config.SessionTTL.Seconds()), Expires: time.Now().Add(s.config.SessionTTL)})
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (Account, bool) {
	var user Account
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) != 64 {
		writeError(w, 401, "session", "Sign in to access your workspace.")
		return user, false
	}

	err = s.store.DB.NewSelect().Model(&user).Join("JOIN sessions AS s ON s.account_id = a.id").Where("s.token_hash = ? AND s.expires_at > now()", tokenHash(cookie.Value)).Scan(r.Context())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 401, "session", "Your session has expired. Sign in again.")
		} else {
			writeError(w, 503, "unavailable", "Authentication is temporarily unavailable.")
		}

		return user, false
	}

	return user, true
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if user, ok := s.currentUser(w, r); ok {
		respond(w, 200, map[string]any{"user": user})
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if _, err := s.store.DB.NewDelete().Model((*Session)(nil)).Where("token_hash = ?", tokenHash(cookie.Value)).Exec(r.Context()); err != nil {
			writeError(w, 503, "unavailable", "Unable to sign out. Try again.")
			return
		}
	}

	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	respond(w, 200, map[string]bool{"signedOut": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)

	if !ok {
		return
	}

	var input struct{ CurrentPassword, NewPassword string }

	if !decode(w, r, &input) {
		return
	}

	if len(input.CurrentPassword) > 72 || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.CurrentPassword)) != nil {
		writeError(w, 401, "credentials", "Current password is incorrect.")
		return
	}

	hash, err := hashPassword(input.NewPassword)
	if err != nil {
		writeError(w, 400, "validation", err.Error())
		return
	}

	err = s.store.DB.RunInTx(r.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*Account)(nil)).Set("password_hash = ?", hash).Where("id = ? AND password_hash = ?", user.ID, user.PasswordHash).Exec(ctx)
		if err != nil {
			return err
		}

		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return errCredentials
		}

		_, err = tx.NewDelete().Model((*Session)(nil)).Where("account_id = ?", user.ID).Exec(ctx)
		return err
	})
	if err != nil {
		writeError(w, 409, "account", "Account changed. Sign in and try again.")
		return
	}

	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	respond(w, 200, map[string]bool{"passwordChanged": true})
}
