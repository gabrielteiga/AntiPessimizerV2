package native

import (
	"bytes"
	"context"
	"testing"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
)

func TestNativeProducerReusesBoundedRingsAcrossThreadLifetimes(t *testing.T) {
	o := frameFixture(t)
	const rounds, workers = 100, 3
	o.Args = []string{"--threads", "3", "--rounds", "100", "--iterations", "1", "--delay-ms", "0"}
	var output bytes.Buffer
	if err := Launch(context.Background(), o, &output); err != nil {
		t.Fatal(err, output.String())
	}
	records, err := instrumentation.ReadMetrics(&output)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1].Metrics
	if last.Incomplete || last.OpenCalls != 0 || last.AbandonedCalls != 0 || len(last.Rows) != rounds*workers*3 {
		t.Fatal("ended lifetimes exhausted buffers or merged rows", last)
	}
	type lifetime struct {
		tid   uint32
		start uint64
	}
	lives := map[lifetime]map[uint32]instrumentation.Row{}
	for _, row := range last.Rows {
		key := lifetime{row.ThreadID, row.ThreadInstance}
		if lives[key] == nil {
			lives[key] = map[uint32]instrumentation.Row{}
		}
		lives[key][row.ProcedureID] = row
	}
	if len(lives) != rounds*workers {
		t.Fatal("thread instances merged", len(lives))
	}
	for _, rows := range lives {
		p, c, r := rows[1], rows[2], rows[3]
		if p.Calls != 2 || c.Calls != 3 || r.Calls != 4 || p.UnwoundCalls != 1 || c.UnwoundCalls != 1 {
			t.Fatal(rows)
		}
		if p.InclusiveTicks != p.ExclusiveTicks+c.InclusiveTicks+r.InclusiveTicks {
			t.Fatal("lifetime timing changed", rows)
		}
	}
	t.Logf("%d actual Windows thread lifetimes captured with a pool of 64 native rings", len(lives))
}
