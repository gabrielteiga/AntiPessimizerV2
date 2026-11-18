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

func TestNativeProducerCompareExchange128SuccessFailureContentionAndFaults(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameCompare128": 1, "APFrameIncrement128": 2, "APFrameCompare128Catch": 3})
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	s := records[len(records)-1].Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 9 {
		t.Fatal(s)
	}
	threads := map[uint32]map[uint32]instrumentation.Row{}
	for _, r := range s.Rows {
		if threads[r.ThreadID] == nil {
			threads[r.ThreadID] = map[uint32]instrumentation.Row{}
		}
		threads[r.ThreadID][r.ProcedureID] = r
	}
	if len(threads) != 3 {
		t.Fatal("workers merged", threads)
	}
	for _, rows := range threads {
		compare, increment, catch := rows[1], rows[2], rows[3]
		if compare.Calls != 24 || compare.UnwoundCalls != 2 || increment.Calls != 11 || increment.UnwoundCalls != 0 || catch.Calls != 2 || catch.UnwoundCalls != 0 {
			t.Fatal(rows)
		}
		if compare.InclusiveTicks != compare.ExclusiveTicks || increment.InclusiveTicks != increment.ExclusiveTicks || catch.InclusiveTicks < catch.ExclusiveTicks || catch.InclusiveTicks-catch.ExclusiveTicks > compare.InclusiveTicks {
			t.Fatal("128-bit atomic times do not balance", rows)
		}
		for _, row := range rows {
			if row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(s.Frequency)/float64(row.Calls) {
				t.Fatal("atomic average changed", row)
			}
		}
	}
	t.Log("three workers: exact 128-bit counter/complement after CAS retry loops; compare/exchange success and failed expected-pair updates preserved; null and misaligned operands unwind through original CRT catch")
}
