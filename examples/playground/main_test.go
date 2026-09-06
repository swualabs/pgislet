package main

import (
	"flag"
	"os"
	"testing"
)

func TestMissingDSN(t *testing.T) {
	t.Setenv("PGISLET_DSN", "")
	args, flags := os.Args, flag.CommandLine
	defer func() {
		os.Args = args
		flag.CommandLine = flags
	}()

	os.Args = []string{"playground"}
	flag.CommandLine = flag.NewFlagSet("playground", flag.ContinueOnError)

	if err := run(); err == nil {
		t.Fatal("expected DSN error")
	}
}
