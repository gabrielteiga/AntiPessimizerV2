package native

import (
	"bytes"
	"context"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerStringCopiesFillsAndContendedAtomics(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameCopy": 1, "APFrameFill": 2, "APLeafAtomic": 3, "APLeafCompare": 4})
	var out bytes.Buffer
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != 12 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		calls := uint64(11)
		unwound := uint64(0)
		if row.Location.Procedure == "APFrameCopy" {
			calls = 12
			unwound = 1 // real access violation inside REP MOVSQ
		}
		if row.Location.Procedure == "APLeafCompare" {
			calls = 22 // compare/exchange success and failure on each invocation
		}
		if row.Calls != calls || row.UnwoundCalls != unwound || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal(row)
		}
	}
	t.Log("native REP copy/fill validates every destination byte including zero/remainder lengths; shared LOCK XADD total and CMPXCHG success/failure preserved on three threads")
}
