package main

import (
	"bytes"
	"context"
	"testing"
)

func TestInvalidRebuildDoesNotOpenDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://invalid")
	for _, args := range [][]string{nil, {"unknown"}, {"rebuild"}, {"rebuild", "--account", "a", "--algorithm", "unknown"}, {"rebuild", "--account", "a", "extra"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("args %v err %v output %s", args, err, out.String())
		}
	}
}
