package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/stream"
)

func TestDemoCLI(t *testing.T) {
	var out, diag bytes.Buffer
	if err := run(context.Background(), []string{"--demo", "--windows", "2", "--samples", "10", "--top", "1"}, &out, &diag); err != nil {
		t.Fatal(err)
	}
	r := stream.NewReader(&out)
	for i := 0; i < 2; i++ {
		s, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if s.Source != "synthetic" || len(s.Hotspots) != 1 || s.OtherSamples != 3 {
			t.Fatal(s)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--demo", "--windows", "0"}, {"--demo", "--top", "0"}, {"--demo", "unexpected"}} {
		if err := run(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatal("accepted invalid args", args)
		}
	}
}
