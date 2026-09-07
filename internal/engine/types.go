package engine

import (
	"time"
)

type Islet struct {
	ID         string
	generation int64
	State      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Column struct {
	Name        string
	DataTypeOID uint32
}

type Result struct {
	Columns      []Column
	Rows         [][]any
	CommandTag   string
	RowsAffected int64
	Duration     time.Duration
	Truncated    bool
}
