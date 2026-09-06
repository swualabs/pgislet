package engine

import (
	"errors"
	"testing"
)

func TestBatchInputBudgets(t *testing.T) {
	manager := &Manager{config: Config{MaxSQLBytes: 10, MaxBatchStatements: 2}}
	cases := []struct {
		name       string
		statements []string
		exceeded   bool
	}{
		{name: "empty"},
		{name: "exact byte budget", statements: []string{"12345", "67890"}},
		{name: "single oversized SQL", statements: []string{"12345678901"}, exceeded: true},
		{name: "combined SQL bytes", statements: []string{"123456", "78901"}, exceeded: true},
		{name: "statement count", statements: []string{"1", "2", "3"}, exceeded: true},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := manager.checkSize(test.statements)
			if errors.Is(err, ErrSQLTooLarge) != test.exceeded {
				t.Fatalf("budget result: %v", err)
			}
		})
	}
}
