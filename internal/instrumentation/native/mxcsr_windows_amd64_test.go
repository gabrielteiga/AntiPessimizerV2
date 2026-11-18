package native

import (
	"bytes"
	"context"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerPreservesMXCSRChangesAndStatus(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APLeafSetControl": 1, "APLeafGetControl": 2})
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 6 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 44 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
	t.Log("all four SSE rounding modes plus exception-status bits preserved across selected setter/getter and native callbacks; original environment restored before other application checks")
}
