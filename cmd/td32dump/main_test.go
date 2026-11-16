package main

import (
	"bytes"
	"testing"
)

func TestRunErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}, {"missing.tds"}} {
		var out bytes.Buffer
		if err := run(args, &out); err == nil || out.Len() != 0 {
			t.Fatalf("args %v: %v", args, err)
		}
	}
}
