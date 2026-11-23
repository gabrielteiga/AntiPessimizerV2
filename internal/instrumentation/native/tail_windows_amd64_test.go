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
	testTailCalls(t, "APTailStart", false)
}
func TestNativeProducerVerifiedStackAndBiasedFrameTailCalls(t *testing.T) {
	testTailCalls(t, "APTailStart", true)
}
func TestNativeProducerVerifiedIndirectTailPreservesArgumentsAndUnwind(t *testing.T) {
	testTailCalls(t, "APTailIndirectStart", false)
}
func TestNativeProducerConditionalIndirectTailMeasuresBothDestinations(t *testing.T) {
	testTailCalls(t, "APTailConditionalStart", false)
}
func testTailCalls(t *testing.T, start string, framed bool) {
	t.Helper()
	names := map[string]uint32{start: 1, "APTailMiddle": 2, "APTailWork": 3, "APTailRecursive": 4, "APTailRecursiveStep": 5, "APTailCatch": 6}
	if framed {
		names["APTailFramedStack"] = 7
		names["APTailFramedRBP"] = 8
	}
	conditional := start == "APTailConditionalStart"
	if conditional {
		names["APTailAlternative"] = 9
	}
	o := selectedFixture(t, "APTailTarget.exe", names)
	o.Args = []string{"--threads", "3", "--iterations", "10"}
	if start == "APTailIndirectStart" {
		o.Args = append(o.Args, "--indirect", "1")
	} else if conditional {
		o.Args = append(o.Args, "--indirect", "2")
	}
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
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 3*len(names) {
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
		counts := map[uint32]uint64{1: 11, 2: 11, 3: 11, 4: 6, 5: 5, 6: 1}
		if framed {
			counts[7] = 11
			counts[8] = 11
		}
		if conditional {
			counts[2], counts[9] = 6, 5
		}
		for id, want := range counts {
			row := r[id]
			unwound := uint64(0)
			if id <= 3 || id == 7 || id == 8 {
				unwound = 1
			}
			if row.Calls != want || row.UnwoundCalls != unwound {
				t.Fatal("wrong tail/recursive/unwind totals", r)
			}
			if row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(s.Frequency)/float64(want) {
				t.Fatal("wrong tail mean", row)
			}
		}
		if r[1].InclusiveTicks != r[1].ExclusiveTicks+r[2].InclusiveTicks+r[9].InclusiveTicks ||
			r[2].InclusiveTicks+r[9].InclusiveTicks != r[2].ExclusiveTicks+r[9].ExclusiveTicks+r[3].InclusiveTicks ||
			r[3].InclusiveTicks != r[3].ExclusiveTicks {
			t.Fatal("tail child times do not balance", r)
		}
		if framed && (r[7].InclusiveTicks != r[7].ExclusiveTicks+r[8].InclusiveTicks || r[8].InclusiveTicks != r[8].ExclusiveTicks+r[1].InclusiveTicks) {
			t.Fatal("restored frame tail times do not balance", r)
		}
		if r[4].InclusiveTicks != r[4].ExclusiveTicks+r[5].ExclusiveTicks {
			t.Fatal("mutually recursive tail time double counted", r)
		}
	}
	t.Logf("%s selected tail chain: six Win64 arguments and original return slot preserved; same-stack mutual recursion counted; one real exception closes each tail frame and resumes CRT catch", start)
}

func TestNativeProducerSelectedTailCallsClearKeepsOpenLogicalChain(t *testing.T) {
	o := selectedFixture(t, "APTailTarget.exe", map[string]uint32{"APTailStart": 1, "APTailMiddle": 2, "APTailWork": 3})
	o.Args = []string{"--threads", "2", "--iterations", "1", "--delay-ms", "700"}
	o.Control = NewControl()
	out := clearWhileOpenWriter{control: o.Control, result: make(chan error, 1)}
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err, out.String())
	}
	if !out.requested {
		t.Fatal("tail chain never published six open logical frames")
	}
	if err := <-out.result; err != nil {
		t.Fatal("tail Clear failed", err)
	}
	records, err := instrumentation.ReadMetrics(&out.Buffer)
	if err != nil {
		t.Fatal(err)
	}
	var epoch uint64
	for _, r := range records {
		if r.Metrics.Epoch > epoch {
			epoch = r.Metrics.Epoch
			if r.Metrics.Incomplete || r.Metrics.OpenCalls != 6 {
				t.Fatal("Clear lost shared physical ownership", r.Metrics)
			}
			for _, row := range r.Metrics.Rows {
				if row.Calls != 0 || row.InclusiveTicks != 0 || row.ExclusiveTicks != 0 {
					t.Fatal("Clear retained completed tail times", row)
				}
			}
		}
	}
	s := records[len(records)-1].Metrics
	if epoch != 2 || s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 6 {
		t.Fatal("tail Clear incomplete", s)
	}
	for _, row := range s.Rows {
		if row.Calls != 2 || row.UnwoundCalls != 1 {
			t.Fatal("Clear lost normal or unwound tail completion", row)
		}
	}
	t.Log("two synchronized Clears keep six logical tail frames open on two physical stacks; normal return and later exception both close each frame exactly once")
}
