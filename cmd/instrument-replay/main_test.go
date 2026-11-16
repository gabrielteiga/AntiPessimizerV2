package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"testing"
)

func TestCLI(t *testing.T) {
	var out, diag bytes.Buffer
	if err := run(context.Background(), []string{"--input", "../../testdata/instrumentation/nested.ndjson"}, nil, &out, &diag); err != nil {
		t.Fatal(err)
	}
	var result instrumentation.Record
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Metrics.Rows[0].Calls != 2 {
		t.Fatal(result, err)
	}
}
func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"unexpected"}, {"--input", "missing.ndjson"}, {"--unknown"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatal(args)
		}
	}
}
