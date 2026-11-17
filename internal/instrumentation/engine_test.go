package instrumentation

import (
	"math"
	"reflect"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

func definitions() []Procedure {
	return []Procedure{{ID: 1, Location: profiling.Location{ModuleID: "build", Module: "lab.exe", Unit: "Lab", Procedure: "A", RVA: "0x0000000000001000", Status: "resolved"}}, {ID: 2, Location: profiling.Location{ModuleID: "build", Module: "lab.exe", Unit: "Lab", Procedure: "B", RVA: "0x0000000000002000", Status: "resolved"}}}
}
func engine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(Config{Frequency: 1000, MaxThreads: 4, MaxDepth: 8, MaxRows: 16}, definitions())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.StartThread(7, 0); err != nil {
		t.Fatal(err)
	}
	return e
}
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func row(t *testing.T, e *Engine, tid, proc uint32) Row {
	t.Helper()
	s, err := e.Snapshot()
	check(t, err)
	for _, r := range s.Rows {
		if r.ThreadID == tid && r.ProcedureID == proc {
			return r
		}
	}
	t.Fatalf("missing row %d/%d: %+v", tid, proc, s)
	return Row{}
}
func TestNestedCallsAndAverages(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 10))
	check(t, e.Enter(7, 2, 15))
	check(t, e.Exit(7, 2, 25))
	check(t, e.Exit(7, 1, 40))
	a, b := row(t, e, 7, 1), row(t, e, 7, 2)
	if a.Calls != 1 || a.InclusiveTicks != 30 || a.ExclusiveTicks != 20 || b.InclusiveTicks != 10 || b.ExclusiveTicks != 10 {
		t.Fatal(a, b)
	}
	if a.AverageInclusiveNS != 30e6 || a.AverageExclusiveNS != 20e6 {
		t.Fatal(a)
	}
	check(t, e.Enter(7, 1, 50))
	check(t, e.Exit(7, 1, 60))
	a = row(t, e, 7, 1)
	if a.Calls != 2 || a.InclusiveTicks != 40 || a.ExclusiveTicks != 30 || a.AverageInclusiveNS != 20e6 || a.AverageExclusiveNS != 15e6 {
		t.Fatal(a)
	}
}
func TestRecursiveInclusiveMatchesReference(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 0))
	check(t, e.Enter(7, 1, 5))
	check(t, e.Exit(7, 1, 15))
	check(t, e.Exit(7, 1, 20))
	a := row(t, e, 7, 1)
	if a.Calls != 2 || a.InclusiveTicks != 20 || a.ExclusiveTicks != 20 || a.AverageInclusiveNS != 10e6 {
		t.Fatal(a)
	}
}
func TestMutualRecursionAndThreadIsolation(t *testing.T) {
	e := engine(t)
	check(t, e.StartThread(8, 0))
	check(t, e.Enter(7, 1, 0))
	check(t, e.Enter(7, 2, 5))
	check(t, e.Enter(7, 1, 10))
	check(t, e.Enter(8, 1, 2))
	check(t, e.Exit(7, 1, 20))
	check(t, e.Exit(7, 2, 25))
	check(t, e.Exit(7, 1, 30))
	check(t, e.Exit(8, 1, 42))
	a, b, c := row(t, e, 7, 1), row(t, e, 7, 2), row(t, e, 8, 1)
	if a.Calls != 2 || a.InclusiveTicks != 30 || a.ExclusiveTicks != 20 || b.InclusiveTicks != 20 || b.ExclusiveTicks != 10 || c.InclusiveTicks != 40 {
		t.Fatal(a, b, c)
	}
}
func TestUnwindClearAndThreadReuse(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 0))
	check(t, e.Enter(7, 2, 10))
	check(t, e.Clear(20))
	check(t, e.Unwind(7, 1, 30))
	check(t, e.Exit(7, 1, 40))
	a, b := row(t, e, 7, 1), row(t, e, 7, 2)
	if a.InclusiveTicks != 20 || a.ExclusiveTicks != 10 || b.InclusiveTicks != 10 || b.UnwoundCalls != 1 || b.Calls != 1 {
		t.Fatal(a, b)
	}
	check(t, e.Enter(7, 1, 50))
	check(t, e.EndThread(7, 60))
	s, err := e.Snapshot()
	check(t, err)
	if s.OpenCalls != 0 || s.AbandonedCalls != 1 || !s.Incomplete {
		t.Fatal(s)
	}
	check(t, e.StartThread(7, 70))
	check(t, e.Enter(7, 1, 70))
	check(t, e.Exit(7, 1, 80))
	s, err = e.Snapshot()
	check(t, err)
	if len(s.Rows) != 3 {
		t.Fatalf("TID reuse merged distinct lifetimes: %+v", s)
	}
}
func TestInvalidEventsDoNotMutate(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 5))
	before, err := e.Snapshot()
	check(t, err)
	for _, op := range []func() error{func() error { return e.Exit(7, 2, 10) }, func() error { return e.Exit(7, 1, 4) }, func() error { return e.Enter(7, 99, 6) }, func() error { return e.Unwind(7, 2, 10) }, func() error { return e.Clear(4) }, func() error { return e.StartThread(7, 10) }, func() error { return e.Exit(99, 1, 10) }} {
		if err := op(); err == nil {
			t.Fatal("invalid event accepted")
		}
		after, err := e.Snapshot()
		check(t, err)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed operation mutated state")
		}
	}
	check(t, e.Exit(7, 1, 10))
}
func TestLimitsAndOverflow(t *testing.T) {
	e, err := New(Config{Frequency: 1, MaxThreads: 1, MaxDepth: 1, MaxRows: 1}, definitions())
	check(t, err)
	check(t, e.StartThread(7, 0))
	check(t, e.Enter(7, 1, 0))
	if err := e.Enter(7, 1, 1); err == nil {
		t.Fatal("depth limit ignored")
	}
	if err := e.StartThread(8, 0); err == nil {
		t.Fatal("thread limit ignored")
	}
	check(t, e.Exit(7, 1, 1))
	if err := e.Enter(7, 2, 2); err == nil {
		t.Fatal("row limit ignored")
	}
	// Raw ticks may be valid while their nanosecond representation overflows.
	if _, err := nanoseconds(math.MaxUint64, 1); err == nil {
		t.Fatal("duration conversion overflow")
	}
	if n, err := nanoseconds(3, 2); err != nil || n != 1500000000 {
		t.Fatal(n, err)
	}
}
func BenchmarkNestedPair(b *testing.B) {
	e, _ := New(Config{Frequency: 10000000, MaxThreads: 1, MaxDepth: 8, MaxRows: 4}, definitions())
	e.StartThread(7, 0)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n := uint64(i) * 10
		e.Enter(7, 1, n)
		e.Enter(7, 2, n+1)
		e.Exit(7, 2, n+4)
		e.Exit(7, 1, n+8)
	}
}

func TestRecursiveIntermediateSnapshotAndClear(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 0))
	check(t, e.Enter(7, 1, 5))
	check(t, e.Exit(7, 1, 15))
	a := row(t, e, 7, 1)
	if a.Calls != 1 || a.InclusiveTicks != 10 || a.ActiveFrames != 1 {
		t.Fatal(a)
	}
	check(t, e.Clear(16))
	check(t, e.Exit(7, 1, 20))
	a = row(t, e, 7, 1)
	if a.Calls != 1 || a.InclusiveTicks != 4 || a.ExclusiveTicks != 4 || a.ActiveFrames != 0 {
		t.Fatal(a)
	}
}
func TestUnwindOverflowRollsBackAllFrames(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 0))
	check(t, e.Enter(7, 2, 5))
	e.rows[key{7, 0, 1}] = totals{Calls: math.MaxUint64}
	before, err := e.Snapshot()
	check(t, err)
	if err := e.Unwind(7, 0, 20); err == nil {
		t.Fatal("call count overflow ignored")
	}
	after, err := e.Snapshot()
	check(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unwind partially committed on overflow")
	}
	if len(e.threads[7].stack) != 2 || e.threads[7].last != 5 {
		t.Fatal("unwind mutated active stack")
	}
}
func TestLossAndConfiguration(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 0))
	check(t, e.MarkLoss())
	s, err := e.Snapshot()
	check(t, err)
	if !s.Incomplete || s.AbandonedCalls != 1 || s.OpenCalls != 0 || len(s.Rows) != 0 {
		t.Fatal(s)
	}
	if err := e.Exit(7, 1, 10); err == nil {
		t.Fatal("exit bridged missing events")
	}
	check(t, e.Clear(20))
	s, err = e.Snapshot()
	check(t, err)
	if s.Epoch != 1 || s.Incomplete || s.AbandonedCalls != 0 {
		t.Fatal(s)
	}
	for _, c := range []Config{{}, {Frequency: 1, MaxThreads: 1, MaxDepth: 0, MaxRows: 1}, {Frequency: 1, MaxThreads: 1, MaxDepth: 1, MaxRows: 999999}} {
		if _, err := New(c, definitions()); err == nil {
			t.Fatal("bad config accepted")
		}
	}
	ps := definitions()
	ps = append(ps, ps[0])
	if _, err := New(Config{Frequency: 1, MaxThreads: 1, MaxDepth: 1, MaxRows: 10}, ps); err == nil {
		t.Fatal("duplicate procedure accepted")
	}
}

func TestClearRejectsDelayedThreadCreation(t *testing.T) {
	e := engine(t)
	check(t, e.Clear(20))
	if err := e.StartThread(8, 10); err == nil {
		t.Fatal("thread event crossed clear boundary backwards")
	}
	check(t, e.StartThread(8, 20))
	check(t, e.Enter(8, 1, 21))
	check(t, e.Exit(8, 1, 25))
}

func TestClearCannotMoveBehindEndedOrLostThreads(t *testing.T) {
	for _, lost := range []bool{false, true} {
		e := engine(t)
		check(t, e.Enter(7, 1, 100))
		check(t, e.Exit(7, 1, 110))
		if lost {
			check(t, e.MarkLoss())
		} else {
			check(t, e.EndThread(7, 120))
		}
		before, err := e.Snapshot()
		check(t, err)
		if err := e.Clear(50); err == nil {
			t.Fatal("clear crossed an already observed event after lifecycle cleanup")
		}
		after, err := e.Snapshot()
		check(t, err)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("failed clear modified retained results")
		}
		check(t, e.Clear(130))
	}
}

// Calls shorter than QPC's tick period may have identical entry/exit ticks.
// Preserve the count and a measured zero; never invent a minimum duration.
func TestCompletedCallsWithinOneClockTick(t *testing.T) {
	e := engine(t)
	for i := 0; i < 10; i++ {
		check(t, e.Enter(7, 1, 100))
		check(t, e.Exit(7, 1, 100))
	}
	check(t, e.EndThread(7, 100))
	s, err := e.Snapshot()
	check(t, err)
	if s.Incomplete || s.OpenCalls != 0 || len(s.Rows) != 1 || s.Rows[0].Calls != 10 || s.Rows[0].InclusiveTicks != 0 || s.Rows[0].ExclusiveTicks != 0 || s.Rows[0].AverageInclusiveNS != 0 || s.Rows[0].AverageExclusiveNS != 0 {
		t.Fatal("clock quantization lost calls or invented time", s)
	}
}

func TestNativeCleanupClosesOnlyMatchingTopFrame(t *testing.T) {
	e := engine(t)
	check(t, e.Enter(7, 1, 10))
	check(t, e.Enter(7, 2, 15))
	before, err := e.Snapshot()
	check(t, err)
	for _, op := range []func() error{
		func() error { return e.ExitUnwound(7, 1, 20) },
		func() error { return e.ExitUnwound(99, 2, 20) },
		func() error { return e.ExitUnwound(7, 2, 14) },
	} {
		if op() == nil {
			t.Fatal("invalid cleanup accepted")
		}
		after, err := e.Snapshot()
		check(t, err)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("invalid cleanup mutated metrics")
		}
	}
	check(t, e.ExitUnwound(7, 2, 25))
	check(t, e.ExitUnwound(7, 1, 30))
	a, b := row(t, e, 7, 1), row(t, e, 7, 2)
	if a.Calls != 1 || b.Calls != 1 || a.UnwoundCalls != 1 || b.UnwoundCalls != 1 || a.InclusiveTicks != 20 || a.ExclusiveTicks != 10 || b.InclusiveTicks != 10 {
		t.Fatal(a, b)
	}
	s, err := e.Snapshot()
	check(t, err)
	if s.Incomplete || s.OpenCalls != 0 || s.AbandonedCalls != 0 {
		t.Fatal(s)
	}
	if e.ExitUnwound(7, 1, 31) == nil {
		t.Fatal("duplicate cleanup accepted")
	}
}
