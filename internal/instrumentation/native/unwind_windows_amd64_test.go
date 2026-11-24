package native

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerForwardsOriginalCleanupAndCollidedUnwind(t *testing.T) {
	for _, mode := range [][2]string{{"0", "0"}, {"1", "0"}, {"0", "1"}, {"1", "1"}} {
		t.Run(fmt.Sprintf("abnormal-%s-normal-%s", mode[0], mode[1]), func(t *testing.T) {
			o := frameSelectionFixture(t, map[string]uint32{"APUnwindParent": 1, "APUnwindThrower": 2, "APUnwindWork": 3, "APUnwindOuter": 4})
			o.Args = append(o.Args, "--cleanup-throws", mode[0], "--normal-cleanup-throws", mode[1])
			var output bytes.Buffer
			if err := Launch(context.Background(), o, &output); err != nil {
				t.Fatal(err, output.String())
			}
			records, err := instrumentation.ReadMetrics(&output)
			if err != nil {
				t.Fatal(err)
			}
			s := records[len(records)-1].Metrics
			if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 12 {
				t.Fatal("original cleanup left incomplete frames", s)
			}
			threads := map[uint32]map[uint32]instrumentation.Row{}
			for _, row := range s.Rows {
				calls := map[uint32]uint64{1: 3, 2: 2, 3: 2, 4: 3}[row.ProcedureID]
				unwound := uint64(1)
				if (row.ProcedureID == 1 || row.ProcedureID == 4) && mode[1] == "1" {
					unwound++
				} else if row.ProcedureID == 3 {
					unwound = 0
					for _, value := range mode {
						if value == "1" {
							unwound++
						}
					}
				}
				// A successful tiny cleanup can complete within one QPC tick.
				// Exception paths still include the actual Windows dispatch/unwind.
				zeroAllowed := row.ProcedureID == 3 && unwound == 0
				if row.Calls != calls || row.UnwoundCalls != unwound || row.InclusiveTicks == 0 && !zeroAllowed || row.ExclusiveTicks > row.InclusiveTicks {
					t.Fatal("cleanup changed native counts/times", row)
				}
				if threads[row.ThreadID] == nil {
					threads[row.ThreadID] = map[uint32]instrumentation.Row{}
				}
				if _, duplicate := threads[row.ThreadID][row.ProcedureID]; duplicate {
					t.Fatal("duplicated cleanup identity", row)
				}
				threads[row.ThreadID][row.ProcedureID] = row
			}
			if len(threads) != 3 {
				t.Fatal("cleanup worker identities merged", threads)
			}
			for _, rows := range threads {
				parent, thrower, work, outer := rows[1], rows[2], rows[3], rows[4]
				if len(rows) != 4 || outer.InclusiveTicks != outer.ExclusiveTicks+parent.ExclusiveTicks+thrower.InclusiveTicks+work.InclusiveTicks || thrower.InclusiveTicks != thrower.ExclusiveTicks || work.InclusiveTicks != work.ExclusiveTicks {
					t.Fatal("cleanup time lost between abandoned frame and active outer frame", rows)
				}
			}
		})
	}
}

func TestNativeProducerFrameRegisterUnwindMatchesOriginalStack(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameFaultRBP": 1})
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	s := records[len(records)-1].Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || len(s.Rows) != 3 {
		t.Fatal("frame-register unwind lost the physical return slot", s)
	}
	for _, row := range s.Rows {
		if row.Calls != 1 || row.UnwoundCalls != 1 || row.InclusiveTicks == 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("biased RBP frame did not close exactly once", row)
		}
	}
}

func TestNativeProducerUnwindsInheritedChainedFrame(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameChain", 0, "1")
}

func TestNativeProducerUnwindsInheritedChainedFrameAcrossPadding(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFramePaddedChain", 8, "2")
}

func testNativeInheritedChainedFrame(t *testing.T, symbol string, gap uint32, mode string) {
	t.Helper()
	o := frameSelectionFixture(t, map[string]uint32{symbol: 1, symbol + "Cold": 2})
	primary, secondary := o.Procedures[0], o.Procedures[1]
	if primary.ID == 2 {
		primary, secondary = secondary, primary
	}
	if primary.RVA+primary.Size+gap != secondary.RVA {
		t.Fatal("wrong gap between runtime ranges", primary, secondary)
	}
	primary.Size += secondary.Size + gap
	o.Procedures = []SelectedProcedure{primary}
	o.Args = []string{"--threads", "3", "--iterations", "1", "--delay-ms", "0", "--check-chain", mode}

	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("Windows failed to unwind the original chained frame", err, string(data))
	}
	var output stressObserver
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal("Windows failed to unwind the instrumented chained frame", err)
	}
	record := output.last
	s := record.Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || record.NativeLostEvents != 0 || len(s.Rows) != 3 {
		t.Fatal("chained unwind left incomplete or missing frames", s)
	}
	for _, row := range s.Rows {
		if row.ProcedureID != primary.ID || row.Calls != 2 || row.UnwoundCalls != 1 || row.InclusiveTicks == 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("chained procedure did not record one return and one unwind per thread", row)
		}
	}
}
