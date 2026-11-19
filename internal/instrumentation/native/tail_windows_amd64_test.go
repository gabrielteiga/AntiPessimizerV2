package native

import (
	"bytes"
	"context"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"os/exec"
	"testing"
	"time"
)

func TestNativeProducerSelectedTailCallsPreserveArgumentsRecursionAndUnwind(t *testing.T) {
	o := selectedFixture(t, "APTailTarget.exe", map[string]uint32{"APTailStart": 1, "APTailMiddle": 2, "APTailWork": 3, "APTailRecursive": 4, "APTailRecursiveStep": 5, "APTailCatch": 6})
	o.Args = []string{"--threads", "3", "--iterations", "10"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("unmodified tail baseline", err, string(data))
	}
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	s := records[len(records)-1].Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 18 {
		t.Fatal("tail calls left unmatched frames", s)
	}
	threads := map[uint32]map[uint32]instrumentation.Row{}
	for _, r := range s.Rows {
		if threads[r.ThreadID] == nil {
			threads[r.ThreadID] = map[uint32]instrumentation.Row{}
		}
		threads[r.ThreadID][r.ProcedureID] = r
	}
	if len(threads) != 3 {
		t.Fatal("tail workers merged", threads)
	}
	for _, r := range threads {
		for id, want := range map[uint32]uint64{1: 11, 2: 11, 3: 11, 4: 6, 5: 5, 6: 1} {
			row := r[id]
			unwound := uint64(0)
			if id <= 3 {
				unwound = 1
			}
			if row.Calls != want || row.UnwoundCalls != unwound {
				t.Fatal("wrong tail/recursive/unwind totals", r)
			}
			if row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(s.Frequency)/float64(want) {
				t.Fatal("wrong tail mean", row)
			}
		}
		if r[1].InclusiveTicks != r[1].ExclusiveTicks+r[2].InclusiveTicks || r[2].InclusiveTicks != r[2].ExclusiveTicks+r[3].InclusiveTicks || r[3].InclusiveTicks != r[3].ExclusiveTicks {
			t.Fatal("tail child times do not balance", r)
		}
		if r[4].InclusiveTicks != r[4].ExclusiveTicks+r[5].ExclusiveTicks {
			t.Fatal("mutually recursive tail time double counted", r)
		}
	}
	t.Log("selected direct tail chain: six Win64 arguments and original return slot preserved; same-stack mutual recursion counted; one real exception closes all three tail frames per worker and resumes CRT catch")
}
