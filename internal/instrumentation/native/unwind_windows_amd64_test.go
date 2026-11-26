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

func TestNativeProducerUnwindsChainedLocalRegisterSave(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameLocalSave", 0, "3")
}

func TestNativeProducerUnwindsMultipleChainedRegisterSaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameMultipleSaves", 0, "4")
}

func TestNativeProducerUnwindsChainedXMMAndGeneralRegisterSaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameMixedSaves", 0, "5")
}

func TestNativeProducerUnwindsChainedUnalignedXMMSaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameUnalignedSaves", 0, "6")
}

func TestNativeProducerForwardsCleanupAcrossInheritedChain(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameCleanupChain", 0, "8")
}

func TestNativeProducerCatchesWithinInheritedChain(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameScopeChain", 0, "9")
}

func TestNativeProducerUnwindsChainedLargeAllocation(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameLargeSaveFrame", 0, "7")
}

func TestNativeProducerCatchesAndCleansWithinInheritedChain(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameFinallyChain", 0, "10")
}

func TestNativeProducerUnwindsChainedFarRegisterSaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameFarSaves", 0, "11")
}

func TestNativeProducerUnwindsChainedOddSlotFarSave(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameFarGPRSave", 0, "12")
}

func TestNativeProducerUnwindsFullWidthXMMMoveVariants(t *testing.T) {
	for _, tc := range []struct{ symbol, mode string }{{"APFrameMOVAPD", "13"}, {"APFrameMOVUPD", "14"}, {"APFrameMOVDQA", "15"}, {"APFrameMOVDQU", "16"}} {
		t.Run(tc.symbol, func(t *testing.T) { testNativeInheritedChainedFrame(t, tc.symbol, 0, tc.mode) })
	}
}

func TestNativeProducerUnwindsLocalSavesWithBiasedFrameRegister(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameBiasedLocalSaves", 0, "17")
}

func TestNativeProducerUnwindsLocalSavesWithHighFrameRegister(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameR12LocalSaves", 0, "18")
}

func TestNativeProducerUnwindsPrimaryAndLocalMemorySaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFramePrimaryMemorySave", 0, "19")
}

func TestNativeProducerUnwindsPrimaryMixedAndLocalMemorySaves(t *testing.T) {
	for _, tc := range []struct{ symbol, mode string }{{"APFramePrimaryMixedSaves", "20"}, {"APFramePrimaryFarSaves", "21"}, {"APFramePrimaryFrameSaves", "22"}} {
		t.Run(tc.symbol, func(t *testing.T) { testNativeInheritedChainedFrame(t, tc.symbol, 0, tc.mode) })
	}
}

func TestNativeProducerUnwindsCallsFromLocalSaveFragment(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameLocalCall", 0, "23")
}

func TestNativeProducerForwardsCleanupFromLocalSaveFragment(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameLocalCleanup", 0, "24")
}

func TestNativeProducerPreservesBranchesInsideLocalSaveBody(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameLocalBranches", 0, "25")
}

func TestNativeProducerUnwindsFrameAddressedLocalSaves(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFrameFrameAddressedSaves", 0, "26")
}

func TestNativeProducerUnwindsLocalSavesAfterUnreachablePadding(t *testing.T) {
	testNativeInheritedChainedFrame(t, "APFramePaddedLocalSaves", 8, "27")
}

func testNativeInheritedChainedFrame(t *testing.T, symbol string, gap uint32, mode string) {
	t.Helper()
	names := map[string]uint32{symbol: 1, symbol + "Cold": 2}
	withStackProbe := mode == "3" || mode == "4" || mode == "5" || mode == "6" || mode == "7" || mode == "11" || mode == "12" || mode == "13" || mode == "14" || mode == "15" || mode == "16" || mode == "17" || mode == "18" || mode == "19" || mode == "20" || mode == "21" || mode == "22" || mode == "23" || mode == "24" || mode == "25" || mode == "26" || mode == "27"
	if withStackProbe {
		names["APFrameStackProbe"] = 3
	}
	o := frameSelectionFixture(t, names)
	var primary, secondary SelectedProcedure
	keep := o.Procedures[:0]
	for _, procedure := range o.Procedures {
		switch procedure.ID {
		case 1:
			primary = procedure
		case 2:
			secondary = procedure
		default:
			keep = append(keep, procedure)
		}
	}
	if primary.RVA+primary.Size+gap != secondary.RVA {
		t.Fatal("wrong gap between runtime ranges", primary, secondary)
	}
	primary.Size += secondary.Size + gap
	o.Procedures = append(keep, primary)
	o.Args = []string{"--threads", "3", "--iterations", "1", "--delay-ms", "0", "--check-chain", mode}
	if withStackProbe {
		o.Args = append(o.Args, "--check-stack-probe", "1")
	}

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
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || record.NativeLostEvents != 0 || len(s.Rows) != 3*len(o.Procedures) {
		t.Fatal("chained unwind left incomplete or missing frames", s)
	}
	wantUnwound := uint64(1)
	if mode == "9" || mode == "10" {
		wantUnwound = 0
	}
	for _, row := range s.Rows {
		if row.ProcedureID != primary.ID && (!withStackProbe || row.ProcedureID != 3) || row.Calls != 2 || row.UnwoundCalls != wantUnwound || row.InclusiveTicks == 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("chained procedure did not preserve expected calls and unwinds per thread", row)
		}
	}
}

func TestNativeProducerIndexedJumpTablePreservesCasesFlagsAndUnwind(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameIndexedTable": 1})
	o.Args = append(o.Args, "--check-indexed-table", "1")
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	if data, err := exec.CommandContext(ctx, o.Image, o.Args...).CombinedOutput(); err != nil {
		t.Fatal("original table did not preserve bounds, flags, or exception", err, string(data))
	}
	var output stressObserver
	if err := Launch(ctx, o, &output); err != nil {
		t.Fatal("instrumented table changed behavior", err)
	}
	s := output.last.Metrics
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 || output.last.NativeLostEvents != 0 || len(s.Rows) != 3 {
		t.Fatal("indexed procedure left incomplete or missing frames", s)
	}
	for _, row := range s.Rows {
		if row.ProcedureID != 1 || row.Calls != 6 || row.UnwoundCalls != 1 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("index paths did not record five returns and one unwind per thread", row)
		}
	}
}
