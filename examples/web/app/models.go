package app

import (
	"time"

	"github.com/uptrace/bun"
)

type Account struct {
	bun.BaseModel `bun:"table:accounts,alias:a"`
	ID            string    `bun:",pk" json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	PasswordHash  string    `json:"-"`
	WorkspaceID   string    `json:"-"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Session struct {
	bun.BaseModel `bun:"table:sessions,alias:s"`
	TokenHash     string `bun:",pk"`
	AccountID     string
	ExpiresAt     time.Time
	CreatedAt     time.Time
}
