package native

import (
	"testing"
)

func TestNativeProducerLargeProcedurePreservesCallsReturnsAndUnwind(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameLarge": 1, "APFrameLargeWork": 2, "APFrameLargeCatch": 3})
	for _, p := range o.Procedures {
		if p.ID == 1 && (p.Size <= 4096 || p.Size > MaxProcedureBytes) {
			t.Fatal("fixture no longer exercises a large function", p)
		}
	}
	s := scopeCapture(t, o)
	for _, rows := range scopeRowsByThread(s) {
		large, work, catch := rows[1], rows[2], rows[3]
		if large.Calls != 12 || large.UnwoundCalls != 1 || work.Calls != 11 || work.UnwoundCalls != 1 || catch.Calls != 1 || catch.UnwoundCalls != 0 {
			t.Fatal("large method lost early return, normal calls or actual unwind", rows)
		}
		if large.InclusiveTicks != large.ExclusiveTicks+work.InclusiveTicks || work.InclusiveTicks != work.ExclusiveTicks {
			t.Fatal("large method timing does not balance", rows)
		}
	}
}
