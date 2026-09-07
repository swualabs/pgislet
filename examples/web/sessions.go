package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/swualabs/pgislet"
)

const cookieName = "pgislet_playground"

const sessionTTL = 30 * time.Minute

const maxSessions = 32

type session struct {
	id   string
	used time.Time
}

func (s *Server) current(w http.ResponseWriter, r *http.Request) (pgislet.Islet, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session", "The workspace has expired. Reconnect to continue.")
		return pgislet.Islet{}, false
	}

	s.mu.Lock()
	item, ok := s.sessions[cookie.Value]

	if ok && time.Since(item.used) < sessionTTL {
		item.used = time.Now()
		s.sessions[cookie.Value] = item
	} else {
		ok = false
	}

	s.mu.Unlock()

	if !ok {
		writeError(w, http.StatusUnauthorized, "session", "The workspace has expired. Reconnect to continue.")
		return pgislet.Islet{}, false
	}

	h, err := s.manager.Open(r.Context(), item.id)
	if err != nil {
		report(w, err, nil)
		return h, false
	}

	setCookie(w, r, cookie.Value)
	return h, true
}

func setCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: int(sessionTTL.Seconds())})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		item, ok := s.sessions[cookie.Value]
		alive := ok && time.Since(item.used) < sessionTTL
		s.mu.Unlock()

		if alive {
			if h, ok := s.current(w, r); ok {
				respond(w, http.StatusOK, h)
			}

			return
		}
	}

	s.mu.Lock()

	if len(s.sessions)+s.pending >= maxSessions {
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "capacity", "All workspace slots are in use. Try again shortly.")
		return
	}

	s.pending++
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.pending--
		s.mu.Unlock()
	}()

	token := make([]byte, 32)
	_, _ = rand.Read(token)
	h, err := s.manager.CreateWithInitialization(r.Context(), seed)
	if err != nil {
		if h.ID != "" {
			_ = s.remove(context.Background(), h.ID)
		}

		report(w, err, nil)
		return
	}

	key := hex.EncodeToString(token)
	s.mu.Lock()
	s.sessions[key] = session{id: h.ID, used: time.Now()}
	s.mu.Unlock()

	setCookie(w, r, key)
	respond(w, http.StatusCreated, h)
}

func (s *Server) end(w http.ResponseWriter, r *http.Request) {
	h, ok := s.current(w, r)

	if !ok {
		return
	}

	if err := s.remove(r.Context(), h.ID); err != nil {
		report(w, err, nil)
		return
	}

	cookie, _ := r.Cookie(cookieName)
	s.mu.Lock()
	delete(s.sessions, cookie.Value)
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	respond(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) remove(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	h, err := s.manager.Open(ctx, id)
	if errors.Is(err, pgislet.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if h.State == "initializing" {
		h, err = s.manager.Recover(ctx, h)
		if err != nil {
			return err
		}
	}

	return s.manager.Delete(ctx, h)
}

func (s *Server) Cleanup(ctx context.Context, all bool) error {
	s.mu.Lock()
	expired := map[string]session{}

	for token, item := range s.sessions {
		if all || time.Since(item.used) >= sessionTTL {
			expired[token] = item
			delete(s.sessions, token)
		}
	}

	s.mu.Unlock()

	var failures []error

	for token, item := range expired {
		if err := s.remove(ctx, item.id); err != nil {
			s.mu.Lock()
			s.sessions[token] = item
			s.mu.Unlock()
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}
