package native

import (
	"bytes"
	"context"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerScalarLeafArithmeticHomeSlotsAndConversions(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APLeafMath": 1, "APLeafConvert": 2, "APFrameSignedAlloc": 3})
	var out bytes.Buffer
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 9 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 11 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
	t.Log("actual native leaf arithmetic, six arguments, Win64 home writes, integer/SSE conversions and double return preserved on three threads")
}
