package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound           = errors.New("islet not found")
	ErrBusy               = errors.New("islet busy")
	ErrUnavailable        = errors.New("islet unavailable")
	ErrFailed             = errors.New("islet failed")
	ErrStaleGeneration    = errors.New("stale generation")
	ErrPolicy             = errors.New("SQL policy violation")
	ErrTimeout            = errors.New("query timeout")
	ErrResultLimit        = errors.New("result limit exceeded")
	ErrSQLTooLarge        = errors.New("SQL too large")
	ErrRuntimeConnection  = errors.New("runtime connection failure")
	ErrInitialization     = errors.New("initialization failure")
	ErrLifecycle          = errors.New("lifecycle failure")
	ErrQuery              = errors.New("PostgreSQL query error")
	ErrOutcomeUnknown     = errors.New("transaction outcome unknown")
	ErrUnsupported        = errors.New("unsupported PostgreSQL environment")
	ErrExternalDependency = errors.New("external dependency prevents safe cleanup")
)

type Error struct {
	Kind      error
	Cause     error
	Statement int
}

func (e *Error) Error() string {
	return fmt.Sprintf("pgislet: %v: %v", e.Kind, e.Cause)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Is(target error) bool {
	return target == e.Kind
}

func wrap(kind, cause error) error {
	return &Error{Kind: kind, Cause: cause, Statement: -1}
}

func classify(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return wrap(ErrTimeout, err)
	}

	if pe, ok := errors.AsType[*pgconn.PgError](err); ok {
		kind := ErrQuery

		switch pe.Code {
		case "55P03":
			kind = ErrBusy
		case "57014":
			kind = ErrTimeout
		case "PI001":
			kind = ErrNotFound
		case "PI002":
			kind = ErrStaleGeneration
		case "PI003":
			kind = ErrUnavailable
		case "PI004":
			kind = ErrFailed
		}

		return wrap(kind, err)
	}

	return err
}

func pgconnError(err error) bool {
	var p *pgconn.PgError
	return errors.As(err, &p)
}
