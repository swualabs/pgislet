package pgislet

import (
	"context"

	"github.com/swualabs/pgislet/internal/engine"
)

type Config = engine.Config

type Manager = engine.Manager

type Islet = engine.Islet

type Column = engine.Column

type Result = engine.Result

type Error = engine.Error

var (
	ErrNotFound           = engine.ErrNotFound
	ErrBusy               = engine.ErrBusy
	ErrUnavailable        = engine.ErrUnavailable
	ErrFailed             = engine.ErrFailed
	ErrStaleGeneration    = engine.ErrStaleGeneration
	ErrPolicy             = engine.ErrPolicy
	ErrTimeout            = engine.ErrTimeout
	ErrResultLimit        = engine.ErrResultLimit
	ErrSQLTooLarge        = engine.ErrSQLTooLarge
	ErrRuntimeConnection  = engine.ErrRuntimeConnection
	ErrInitialization     = engine.ErrInitialization
	ErrLifecycle          = engine.ErrLifecycle
	ErrQuery              = engine.ErrQuery
	ErrOutcomeUnknown     = engine.ErrOutcomeUnknown
	ErrUnsupported        = engine.ErrUnsupported
	ErrExternalDependency = engine.ErrExternalDependency
)

func New(ctx context.Context, cfg Config) (*Manager, error) {
	return engine.New(ctx, cfg)
}
