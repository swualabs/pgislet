package engine

import (
	"fmt"
	"time"
)

type Config struct {
	DSN                    string
	OperationTimeout       time.Duration
	StatementTimeout       time.Duration
	LockTimeout            time.Duration
	IdleTransactionTimeout time.Duration
	MaxSQLBytes            int
	MaxBatchStatements     int
	MaxRows                int
	MaxResultBytes         int
}

func (cfg Config) normalized() (Config, error) {
	if cfg.OperationTimeout == 0 {
		cfg.OperationTimeout = 30 * time.Second
	}

	if cfg.StatementTimeout == 0 {
		cfg.StatementTimeout = 10 * time.Second
	}

	if cfg.LockTimeout == 0 {
		cfg.LockTimeout = time.Second
	}

	if cfg.IdleTransactionTimeout == 0 {
		cfg.IdleTransactionTimeout = 15 * time.Second
	}

	if cfg.MaxSQLBytes == 0 {
		cfg.MaxSQLBytes = 1 << 20
	}

	if cfg.MaxBatchStatements == 0 {
		cfg.MaxBatchStatements = 100
	}

	if cfg.MaxRows == 0 {
		cfg.MaxRows = 1000
	}

	if cfg.MaxResultBytes == 0 {
		cfg.MaxResultBytes = 4 << 20
	}

	if cfg.OperationTimeout < time.Millisecond || cfg.StatementTimeout < time.Millisecond || cfg.LockTimeout < time.Millisecond || cfg.IdleTransactionTimeout < time.Millisecond || cfg.MaxSQLBytes < 1 || cfg.MaxBatchStatements < 1 || cfg.MaxRows < 1 || cfg.MaxResultBytes < 1 {
		return cfg, fmt.Errorf("pgislet: invalid limits")
	}

	return cfg, nil
}
