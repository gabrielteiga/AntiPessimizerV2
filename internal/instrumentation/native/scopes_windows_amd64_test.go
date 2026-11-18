package native

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerResumesSelectedCatchFrames(t *testing.T) {
	cases := []struct {
		name           string
		methods        map[string]uint32
		calls, unwound map[uint32]uint64
	}{
		{"framed", map[string]uint32{"APFrameCatch": 1, "APFrameParent": 2, "APFrameChild": 3}, map[uint32]uint64{1: 1, 2: 11, 3: 21}, map[uint32]uint64{2: 1, 3: 1}},
		{"memory", map[string]uint32{"APFrameMemoryCatch": 1, "APFrameCopy": 2}, map[uint32]uint64{1: 1, 2: 12}, map[uint32]uint64{2: 1}},
		{"frame-register", map[string]uint32{"APFrameRBPCatch": 1, "APFrameFaultRBP": 2}, map[uint32]uint64{1: 1, 2: 1}, map[uint32]uint64{2: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := frameSelectionFixture(t, tc.methods)
			s := scopeCapture(t, o)
			for _, r := range s.Rows {
				if r.Calls != tc.calls[r.ProcedureID] || r.UnwoundCalls != tc.unwound[r.ProcedureID] {
					t.Fatal("catch/resume changed calls", r)
				}
			}
			if tc.name == "frame-register" {
				rows := scopeRowsByThread(s)
				for _, r := range rows {
					if r[1].InclusiveTicks != r[1].ExclusiveTicks+r[2].InclusiveTicks {
						t.Fatal("catch abandoned or duplicated the resumed frame", r)
					}
				}
			}
		})
	}
}
func TestNativeProducerResumesCatchAfterCollidedCleanup(t *testing.T) {
	for _, mode := range [][2]string{{"0", "0"}, {"1", "0"}, {"0", "1"}, {"1", "1"}} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			o := frameSelectionFixture(t, map[string]uint32{"APUnwindCatch": 1, "APUnwindOuter": 2, "APUnwindParent": 3, "APUnwindThrower": 4, "APUnwindWork": 5})
			o.Args = append(o.Args, "--cleanup-throws", mode[0], "--normal-cleanup-throws", mode[1])
			s := scopeCapture(t, o)
			caught, abandoned := uint64(1), uint64(1)
			if mode[1] == "1" {
				caught++
				abandoned++
			}
			for _, r := range scopeRowsByThread(s) {
				if r[1].Calls != caught || r[1].UnwoundCalls != 0 || r[2].Calls != 3 || r[2].UnwoundCalls != abandoned || r[3].Calls != 3 || r[3].UnwoundCalls != abandoned || r[4].Calls != 2 || r[4].UnwoundCalls != 1 || r[5].Calls != 2 {
					t.Fatal("collision closed the target catch or lost a selected frame", r)
				}
				if r[2].InclusiveTicks != r[2].ExclusiveTicks+r[3].ExclusiveTicks+r[4].InclusiveTicks+r[5].InclusiveTicks {
					t.Fatal("collision timing changed", r)
				}
			}
		})
	}
}
func scopeCapture(t *testing.T, o LaunchOptions) instrumentation.Snapshot {
	t.Helper()
	var out bytes.Buffer
	if err := Launch(context.Background(), o, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	s := records[len(records)-1].Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 3*len(o.Procedures) {
		t.Fatal("catch/resume capture incomplete", s)
	}
	return s
}
func scopeRowsByThread(s instrumentation.Snapshot) map[uint32]map[uint32]instrumentation.Row {
	rows := map[uint32]map[uint32]instrumentation.Row{}
	for _, r := range s.Rows {
		if rows[r.ThreadID] == nil {
			rows[r.ThreadID] = map[uint32]instrumentation.Row{}
		}
		rows[r.ThreadID][r.ProcedureID] = r
	}
	return rows
}

func TestNativeProducerCombinedScopesRecursiveCatchAndContinuation(t *testing.T) {
	for _, mode := range [][2]string{{"0", "0"}, {"1", "0"}, {"0", "1"}, {"1", "1"}} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			o := selectedFixture(t, "APScopeTarget.exe", map[string]uint32{"APScopeCombined": 1, "APScopeRecursive": 2, "APScopeThrower": 3, "APScopeCleanup": 4, "APScopeContinue": 5, "APScopeRoot": 6, "APScopeDriver": 7})
			o.Args = []string{"--threads", "3", "--cleanup-throws", mode[0], "--normal-cleanup-throws", mode[1]}
			// The same original executable independently checks its return values and
			// cleanup counts with and without any adapter installed.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
				t.Fatal("baseline scope program", err, string(out))
			}
			s := scopeCapture(t, o)
			cleanupUnwinds := uint64(0)
			for _, v := range mode {
				if v == "1" {
					cleanupUnwinds++
				}
			}
			for _, r := range scopeRowsByThread(s) {
				for id, calls := range map[uint32]uint64{1: 4, 2: 3, 3: 4, 4: 3, 5: 1, 6: 1, 7: 1} {
					wantUnwind := map[uint32]uint64{1: 1, 3: 3, 4: cleanupUnwinds}[id]
					if r[id].Calls != calls || r[id].UnwoundCalls != wantUnwind {
						t.Fatal("search/resume/continuation corrupted physical frame accounting", r)
					}
				}
				if r[3].InclusiveTicks != r[3].ExclusiveTicks || r[4].InclusiveTicks != r[4].ExclusiveTicks || r[5].InclusiveTicks != r[5].ExclusiveTicks {
					t.Fatal("leaf timing changed", r)
				}
				var exclusive uint64
				for _, row := range r {
					exclusive += row.ExclusiveTicks
				}
				if r[7].InclusiveTicks != exclusive {
					t.Fatal("all selected calls must balance beneath the single driver", r)
				}
			}
		})
	}
}
