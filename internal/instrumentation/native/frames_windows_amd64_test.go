package native

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func frameFixture(t *testing.T) LaunchOptions {
	return frameSelectionFixture(t, map[string]uint32{"APFrameParent": 1, "APFrameChild": 2, "APFrameRecursive": 3})
}
func frameSelectionFixture(t *testing.T, names map[string]uint32) LaunchOptions {
	return selectedFixture(t, "APFrameTarget.exe", names)
}
func selectedFixture(t *testing.T, filename string, names map[string]uint32) LaunchOptions {
	t.Helper()
	directory := filepath.Dir(labPath(t))
	path := filepath.Join(directory, filename)
	image, err := pe.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	h := image.OptionalHeader.(*pe.OptionalHeader64)
	directoryEntry := h.DataDirectory[3]
	var table []byte
	for _, s := range image.Sections {
		if directoryEntry.VirtualAddress >= s.VirtualAddress && uint64(directoryEntry.VirtualAddress-s.VirtualAddress)+uint64(directoryEntry.Size) <= uint64(s.Size) {
			data, err := s.Data()
			if err != nil {
				t.Fatal(err)
			}
			at := directoryEntry.VirtualAddress - s.VirtualAddress
			table = data[at : at+directoryEntry.Size]
		}
	}
	o := LaunchOptions{Image: path, DLL: filepath.Join(directory, "AntiPessimizerNative.dll"), Args: []string{"--threads", "3", "--iterations", "10"}, Capacity: 1024, Timeout: 20 * time.Second}
	for _, symbol := range image.Symbols {
		id := names[symbol.Name]
		if id == 0 || symbol.SectionNumber < 1 || int(symbol.SectionNumber) > len(image.Sections) {
			continue
		}
		rva := image.Sections[symbol.SectionNumber-1].VirtualAddress + symbol.Value
		for at := 0; at+12 <= len(table); at += 12 {
			if binary.LittleEndian.Uint32(table[at:]) == rva {
				end := binary.LittleEndian.Uint32(table[at+4:])
				o.Procedures = append(o.Procedures, SelectedProcedure{ID: id, RVA: rva, Size: end - rva, Name: symbol.Name})
			}
		}
	}
	if len(o.Procedures) != len(names) {
		t.Fatal("framed test procedures missing", o.Procedures)
	}
	return o
}

type openFrameObserver struct {
	bytes.Buffer
	observedOpen bool
}

func (w *openFrameObserver) Write(b []byte) (int, error) {
	var record instrumentation.Record
	if err := json.Unmarshal(b, &record); err != nil {
		return 0, err
	}
	if record.Metrics.OpenCalls > 0 && !record.Metrics.Incomplete {
		w.observedOpen = true
	}
	return w.Buffer.Write(b)
}

func TestNativeProducerLongCallPublishesOpenFramesBeforeReturn(t *testing.T) {
	o := frameFixture(t)
	o.Args = []string{"--threads", "1", "--iterations", "1", "--delay-ms", "700"}
	var output openFrameObserver
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	if !output.observedOpen {
		t.Fatal("long call left the independent viewer without live snapshots")
	}
	records, err := instrumentation.ReadMetrics(&output.Buffer)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || len(last.Rows) != 3 {
		t.Fatal("heartbeat altered measured completeness", last)
	}
	for _, row := range last.Rows {
		want := map[uint32]uint64{1: 2, 2: 3, 3: 4}[row.ProcedureID]
		if row.Calls != want {
			t.Fatal("heartbeat invented completed calls", row)
		}
	}
}
func TestNativeProducerFramedCallsRecursionAndRealExceptionUnwind(t *testing.T) {
	o := frameFixture(t)
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
		t.Fatal("framed/SEH capture incomplete", s)
	}
	threads := map[uint32]map[uint32]instrumentation.Row{}
	for _, row := range s.Rows {
		if threads[row.ThreadID] == nil {
			threads[row.ThreadID] = map[uint32]instrumentation.Row{}
		}
		threads[row.ThreadID][row.ProcedureID] = row
	}
	if len(threads) != 3 {
		t.Fatal("framed workers merged")
	}
	for _, rows := range threads {
		p, c, r := rows[1], rows[2], rows[3]
		if p.Calls != 11 || c.Calls != 21 || r.Calls != 40 || p.UnwoundCalls != 1 || c.UnwoundCalls != 1 || r.UnwoundCalls != 0 {
			t.Fatal("wrong native unwind/recursive counts", rows)
		}
		if p.InclusiveTicks != p.ExclusiveTicks+c.InclusiveTicks+r.InclusiveTicks || c.InclusiveTicks != c.ExclusiveTicks || r.InclusiveTicks != r.ExclusiveTicks {
			t.Fatal("native framed times do not balance", rows)
		}
		if p.AverageInclusiveNS != float64(p.InclusiveTicks)*1e9/float64(s.Frequency)/11 {
			t.Fatal("framed averages changed")
		}
	}
	t.Log("ordinary framed methods: native calls/recursion preserved; actual Windows exception unwound two selected frames per thread and continued in the original outer handler")
}

func TestNativeProducerTinyMethodCountsWithoutPatchingAdjacentCode(t *testing.T) {
	o := frameSelectionFixture(t, map[string]uint32{"APFrameTiny": 1})
	if len(o.Procedures) != 1 || o.Procedures[0].Size != 6 {
		t.Fatal("fixture no longer exercises a six-byte routine", o.Procedures)
	}
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || len(last.Rows) != 3 {
		t.Fatal(last)
	}
	for _, row := range last.Rows {
		if row.Calls != 11 || row.UnwoundCalls != 0 || row.InclusiveTicks != row.ExclusiveTicks {
			t.Fatal("tiny procedure changed behavior/counts", row)
		}
	}
}
