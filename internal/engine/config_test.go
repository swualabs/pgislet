package engine

import (
	"testing"
	"time"
)

func TestConfigurationDefaults(t *testing.T) {
	cfg, err := (Config{DSN: "postgres://example"}).normalized()
	if err != nil {
		t.Fatal(err)
	}

	want := Config{
		DSN:                    "postgres://example",
		OperationTimeout:       30 * time.Second,
		StatementTimeout:       10 * time.Second,
		LockTimeout:            time.Second,
		IdleTransactionTimeout: 15 * time.Second,
		MaxSQLBytes:            1 << 20,
		MaxBatchStatements:     100,
		MaxRows:                1000,
		MaxResultBytes:         4 << 20,
	}

	if cfg != want {
		t.Fatalf("defaults: got %+v, want %+v", cfg, want)
	}
}

func TestConfigurationOverrides(t *testing.T) {
	want := Config{
		OperationTimeout:       time.Second,
		StatementTimeout:       time.Second,
		LockTimeout:            time.Second,
		IdleTransactionTimeout: time.Second,
		MaxSQLBytes:            20,
		MaxBatchStatements:     2,
		MaxRows:                5,
		MaxResultBytes:         100,
	}

	got, err := want.normalized()
	if err != nil || got != want {
		t.Fatalf("overrides changed: %+v %v", got, err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{OperationTimeout: -time.Second},
		{StatementTimeout: -time.Second},
		{LockTimeout: time.Nanosecond},
		{IdleTransactionTimeout: -time.Second},
		{MaxSQLBytes: -1},
		{MaxRows: -1},
		{MaxResultBytes: -1},
		{MaxBatchStatements: -1},
	} {
		if _, err := cfg.normalized(); err == nil {
			t.Fatalf("invalid configuration accepted: %+v", cfg)
		}
	}
}
