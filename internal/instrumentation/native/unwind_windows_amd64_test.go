package native

import (
	"bytes"
	"context"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerForwardsOriginalCleanupAndCollidedUnwind(t *testing.T) {
	for _, collision := range []string{"0", "1"} {
		t.Run("cleanup-throws-"+collision, func(t *testing.T) {
			o := frameSelectionFixture(t, map[string]uint32{"APUnwindParent": 1, "APUnwindThrower": 2, "APUnwindWork": 3})
			o.Args = append(o.Args, "--cleanup-throws", collision)
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
				t.Fatal("original cleanup left incomplete frames", s)
			}
			threads := map[uint32]map[uint32]bool{}
			for _, row := range s.Rows {
				calls := map[uint32]uint64{1: 3, 2: 2, 3: 2}[row.ProcedureID]
				unwound := uint64(1)
				if row.ProcedureID == 3 && collision == "0" {
					unwound = 0
				}
				if row.Calls != calls || row.UnwoundCalls != unwound || row.InclusiveTicks == 0 || row.ExclusiveTicks > row.InclusiveTicks {
					t.Fatal("cleanup changed native counts/times", row)
				}
				if threads[row.ThreadID] == nil {
					threads[row.ThreadID] = map[uint32]bool{}
				}
				if threads[row.ThreadID][row.ProcedureID] {
					t.Fatal("duplicated cleanup identity", row)
				}
				threads[row.ThreadID][row.ProcedureID] = true
			}
			if len(threads) != 3 {
				t.Fatal("cleanup worker identities merged", threads)
			}
		})
	}
}
