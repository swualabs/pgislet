package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/swualabs/pgislet"
	"github.com/uptrace/bun"
)

func (s *Server) current(w http.ResponseWriter, r *http.Request) (pgislet.Islet, bool) {
	user, ok := s.currentUser(w, r)

	if !ok {
		return pgislet.Islet{}, false
	}

	if user.WorkspaceID == "" {
		writeError(w, 409, "workspace", "Open your workspace before running SQL.")
		return pgislet.Islet{}, false
	}

	h, err := s.manager.Open(r.Context(), user.WorkspaceID)
	if err != nil {
		report(w, err, nil)
		return h, false
	}

	return h, true
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(w, r)

	if !ok {
		return
	}

	var h pgislet.Islet
	created := ""
	err := s.store.DB.RunInTx(r.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		var locked Account

		if err := tx.NewSelect().Model(&locked).Where("id = ?", user.ID).For("UPDATE NOWAIT").Scan(ctx); err != nil {
			return err
		}

		var err error

		if locked.WorkspaceID != "" {
			h, err = s.manager.Open(ctx, locked.WorkspaceID)
			return err
		}

		h, err = s.manager.CreateWithInitialization(ctx, seed)
		created = h.ID
		if err != nil {
			return err
		}

		_, err = tx.NewUpdate().Model((*Account)(nil)).Set("workspace_id = ?", h.ID).Where("id = ?", user.ID).Exec(ctx)
		return err
	})
	if err != nil {
		if created != "" {
			s.compensate(user.ID, created)
		}

		var pgerr *pgconn.PgError

		if errors.As(err, &pgerr) && pgerr.Code == "55P03" {
			writeError(w, 409, "busy", "Your workspace is being prepared. Try again shortly.")
		} else {
			report(w, err, nil)
		}

		return
	}

	respond(w, http.StatusOK, h)
}

func (s *Server) compensate(userID, isletID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var user Account

	if err := s.store.DB.NewSelect().Model(&user).Where("id = ?", userID).Scan(ctx); err != nil {
		slog.Error("workspace mapping could not be verified; manual reconciliation required", "islet_id", isletID)
		return
	}

	if user.WorkspaceID == isletID {
		return
	}

	h, err := s.manager.Open(ctx, isletID)
	if errors.Is(err, pgislet.ErrNotFound) {
		return
	}

	if err == nil && h.State == "initializing" {
		h, err = s.manager.Recover(ctx, h)
	}

	if err == nil {
		err = s.manager.Delete(ctx, h)
	}

	if err != nil {
		slog.Error("workspace cleanup requires reconciliation", "islet_id", isletID)
	}
}
