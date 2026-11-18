package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/testfixture"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
)

func callRecords(t *testing.T) []instrumentation.Record {
	t.Helper()
	data, err := os.ReadFile("../../testdata/instrumentation/nested.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = instrumentation.Replay(context.Background(), bytes.NewReader(data), &out, instrumentation.Config{MaxThreads: 16, MaxDepth: 32, MaxRows: 64}); err != nil {
		t.Fatal(err)
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	for i := range records {
		records[i].Source = "native-hook"
		records[i].Clock = "qpc"
	}
	return records
}

func TestPreflightRejectionPreservesHistoryAndNeverStartsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsupported.exe")
	data := testfixture.PlannableImage()
	data[0x220], data[0x221] = 0x0f, 0x0b
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := native.ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	c := New()
	c.LaunchInstrumented = func(context.Context, native.LaunchOptions, io.Writer) error {
		t.Error("rejected procedure launched")
		return nil
	}
	previous := callRecords(t)[0]
	if err := c.publishMetrics(previous); err != nil {
		t.Fatal(err)
	}
	state := c.Status()
	page, err := c.Catalog(CatalogQuery{Image: path, Check: true})
	if err != nil || len(page.Checks) != 1 || page.Checks[0].PlanSupported || page.Checks[0].Reason == "" {
		t.Fatal(page, err)
	}
	err = c.StartInstrumented(context.Background(), InstrumentOptions{Image: path, ImageID: catalog.ImageID, CatalogID: catalog.CatalogID, Procedures: []uint32{catalog.Procedures[0].ID}})
	if err == nil || !strings.Contains(err.Error(), "preflight rejected") || c.Status() != state || len(c.MetricHistory()) != 1 || c.cancel != nil {
		t.Fatal("rejection changed lifecycle/history", c.Status(), err)
	}
}

func TestCatalogPreflightChecksOnlyRequestedPage(t *testing.T) {
	c, o := instrumentController(t)
	var checked []uint32
	c.CheckCatalog = func(_ native.Catalog, procedures []native.SelectedProcedure) ([]native.ProcedureCheck, error) {
		for _, p := range procedures {
			checked = append(checked, p.ID)
		}
		return []native.ProcedureCheck{{ID: 2, PlanSupported: true}}, nil
	}
	page, err := c.Catalog(CatalogQuery{Image: o.Image, Offset: 1, Limit: 1, Check: true})
	if err != nil || len(checked) != 1 || checked[0] != 2 || len(page.Checks) != 1 || page.Checks[0].ID != 2 {
		t.Fatal("planned outside requested page", page, checked, err)
	}
}

func TestCatalogAliasSearchHasBoundedMetadataAndOneSelection(t *testing.T) {
	c, o := instrumentController(t)
	load := c.LoadCatalog
	c.LoadCatalog = func(path string) (native.Catalog, error) {
		catalog, err := load(path)
		for i := 0; i < 1000; i++ {
			catalog.Aliases = append(catalog.Aliases, native.ProcedureAlias{ID: 1, Name: fmt.Sprintf("Alternate%d", i), Unit: "Shared"})
		}
		return catalog, err
	}
	page, err := c.Catalog(CatalogQuery{Image: o.Image, Text: "Alternate999", Unit: "Shared", Check: true})
	if err != nil || page.Total != 1 || len(page.Procedures) != 1 || page.Procedures[0].ID != 1 || len(page.Checks) != 1 || page.AliasTotal != 1000 || len(page.Aliases) != 512 {
		t.Fatal("alias lookup duplicated/omitted procedure or unbounded metadata", page.Total, page.AliasTotal, len(page.Aliases), err)
	}
	page.Aliases[0].Name = "changed"
	again, err := c.Catalog(CatalogQuery{Image: o.Image, Text: "Alternate999"})
	if err != nil || again.Aliases[0].Name != "Alternate0" {
		t.Fatal("alias response mutation changed catalog", err)
	}
}
func instrumentController(t *testing.T) (*Controller, InstrumentOptions) {
	t.Helper()
	c := New()
	c.CheckCatalog = func(_ native.Catalog, procedures []native.SelectedProcedure) ([]native.ProcedureCheck, error) {
		checks := make([]native.ProcedureCheck, 0, len(procedures))
		for _, p := range procedures {
			checks = append(checks, native.ProcedureCheck{ID: p.ID, PlanSupported: true})
		}
		return checks, nil
	}
	c.LoadCatalog = func(string) (native.Catalog, error) {
		return native.Catalog{CatalogID: "catalog-token", ImageID: "checked-image", Procedures: []native.SelectedProcedure{{ID: 1, RVA: 0x1000, Size: 32, Name: "Lab.Parent", Unit: "Lab"}, {ID: 2, RVA: 0x2000, Size: 32, Name: "Lab.Child", Unit: "Lab"}}}, nil
	}
	return c, InstrumentOptions{Image: "owned.exe", ImageID: "checked-image", CatalogID: "catalog-token", Procedures: []uint32{1, 2}}
}
func awaitInstrumentedStop(t *testing.T, c *Controller) {
	t.Helper()
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("instrumented owner did not stop")
	}
}
func TestInstrumentedOwnerRetainsCoalescedHistoryAndRejectsConflictingStarts(t *testing.T) {
	records := callRecords(t)
	c, o := instrumentController(t)
	ready := make(chan struct{})
	release := make(chan struct{})
	c.LaunchInstrumented = func(ctx context.Context, options native.LaunchOptions, out io.Writer) error {
		if options.ImageID != "checked-image" || len(options.Procedures) != 2 || options.Procedures[1].ID != 2 {
			return errors.New("selection changed")
		}
		close(ready)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
		}
		for i := uint64(1); i <= 300; i++ {
			r := records[0].Clone()
			r.Sequence = i
			r.At += i
			if err := instrumentation.WriteRecord(out, r); err != nil {
				return err
			}
		}
		return nil
	}
	slow, unsubscribe := c.SubscribeMetrics()
	defer unsubscribe()
	if err := c.StartInstrumented(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	session := c.Status().SessionID
	<-ready
	if err := c.StartInstrumented(context.Background(), o); err == nil {
		t.Fatal("two native owners started")
	}
	if err := c.Start(context.Background(), StartOptions{Demo: true}); err == nil {
		t.Fatal("sampling overlapped instrumentation")
	}
	close(release)
	awaitInstrumentedStop(t, c)
	state, last := c.MetricView()
	if state.State != "stopped" || state.Mode != "instrumentation" || last == nil || last.SessionID != session || last.Sequence != 300 || last.Metrics.Rows[0].Calls != 2 {
		t.Fatal(state, last)
	}
	if len(slow) != 1 {
		t.Fatal("slow client blocked/grew queue")
	}
	if (<-slow).Sequence != 300 {
		t.Fatal("coalescing lost latest cumulative counts")
	}
	history := c.MetricHistory()
	if len(history) != 256 || history[0].Sequence != 45 || history[255].Sequence != 300 {
		t.Fatal("retention incorrect", len(history))
	}
	history[255].Metrics.Rows[0].Calls = 999
	_, last = c.MetricView()
	if last.Metrics.Rows[0].Calls != 2 {
		t.Fatal("client mutated retained totals")
	}
	if c.Latest() != nil {
		t.Fatal("metrics exposed as CPU samples")
	}
	if err := c.Start(context.Background(), StartOptions{Demo: true}); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	if len(c.MetricHistory()) != 0 || c.Status().SessionID == session {
		t.Fatal("restart retained old native identity")
	}
}
func TestInstrumentedStopWaitsForOwnedLaunchAndKeepsIncompleteFinalRecord(t *testing.T) {
	c, o := instrumentController(t)
	records := callRecords(t)
	ready := make(chan struct{})
	released := make(chan struct{})
	c.LaunchInstrumented = func(ctx context.Context, _ native.LaunchOptions, out io.Writer) error {
		defer close(released)
		close(ready)
		<-ctx.Done()
		r := records[0]
		r.Metrics.Incomplete = true
		if err := instrumentation.WriteRecord(out, r); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := c.StartInstrumented(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	<-ready
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	default:
		t.Fatal("Stop returned before launch released owned process")
	}
	status, last := c.MetricView()
	if status.State != "stopped" || last == nil || !last.Metrics.Incomplete {
		t.Fatal(status, last)
	}
}
func TestInstrumentedMalformedCaptureAndEmptyCaptureFail(t *testing.T) {
	for _, payload := range []string{"", "{bad}\n"} {
		c, o := instrumentController(t)
		c.LaunchInstrumented = func(context.Context, native.LaunchOptions, io.Writer) error { return nil }
		if payload != "" {
			c.LaunchInstrumented = func(_ context.Context, _ native.LaunchOptions, out io.Writer) error {
				_, err := io.WriteString(out, payload)
				return err
			}
		}
		if err := c.StartInstrumented(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		awaitInstrumentedStop(t, c)
		if c.Status().State != "error" {
			t.Fatal("invalid capture advertised success", c.Status())
		}
	}
}
func TestCatalogPagingAndChangedBuildNeverLaunch(t *testing.T) {
	c, o := instrumentController(t)
	launched := false
	c.LaunchInstrumented = func(context.Context, native.LaunchOptions, io.Writer) error { launched = true; return nil }
	page, err := c.Catalog(CatalogQuery{Image: o.Image, Text: "lab", Offset: 1, Limit: 1})
	if err != nil || page.Total != 2 || len(page.Procedures) != 1 || page.Procedures[0].ID != 2 {
		t.Fatal(page, err)
	}
	page, err = c.Catalog(CatalogQuery{Image: o.Image, Unit: "other"})
	if err != nil || page.Total != 0 {
		t.Fatal(page, err)
	}
	for _, bad := range []InstrumentOptions{
		{Image: o.Image, ImageID: "old", CatalogID: o.CatalogID, Procedures: []uint32{1}},
		{Image: o.Image, ImageID: o.ImageID, CatalogID: o.CatalogID, Procedures: []uint32{1, 1}},
		{Image: o.Image, ImageID: o.ImageID, CatalogID: o.CatalogID, Procedures: []uint32{99}},
		{Image: o.Image, ImageID: o.ImageID, CatalogID: o.CatalogID},
		{Image: o.Image, ImageID: o.ImageID, CatalogID: "old-catalog", Procedures: []uint32{1}},
	} {
		if err := c.StartInstrumented(context.Background(), bad); err == nil {
			t.Fatal("invalid selection started")
		}
	}
	if launched || c.Status().State != "idle" {
		t.Fatal("validation changed lifecycle")
	}
}
func TestInstrumentedRPCObserverDisconnectDoesNotCancelOwner(t *testing.T) {
	c, o := instrumentController(t)
	records := callRecords(t)
	connected := make(chan struct{})
	c.LaunchInstrumented = func(ctx context.Context, _ native.LaunchOptions, out io.Writer) error {
		if err := instrumentation.WriteRecord(out, records[0]); err != nil {
			return err
		}
		close(connected)
		<-ctx.Done()
		return ctx.Err()
	}
	address := filepath.Join(t.TempDir(), "private", "instrument.sock")
	if runtime.GOOS == "windows" {
		address = fmt.Sprintf(`\\.\pipe\ap-instrument-service-%d`, time.Now().UnixNano())
	}
	listener, err := transport.Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, c) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if _, err = Call(ctx, address, Request{Op: "instrument-start", Instrument: o}); err != nil {
		t.Fatal(err)
	}
	<-connected
	conn, err := transport.Dial(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	if err = transport.Write(conn, Request{Version: Version, Op: "instrument-subscribe"}); err != nil {
		t.Fatal(err)
	}
	var r Response
	if err = transport.Read(conn, &r); err != nil || r.Metric == nil {
		t.Fatal(r, err)
	}
	conn.Close()
	r, err = Call(ctx, address, Request{Op: "latest"})
	if err != nil || r.Status.State != "running" || r.Snapshot != nil || r.Metric == nil {
		t.Fatal("viewer owned capture lifetime", r, err)
	}
	r, err = Call(ctx, address, Request{Op: "instrument-history"})
	if err != nil || len(r.Metrics) != 1 {
		t.Fatal(r, err)
	}
	if _, err = Call(ctx, address, Request{Op: "stop"}); err != nil {
		t.Fatal(err)
	}
	if c.Status().State != "stopped" {
		t.Fatal(c.Status())
	}
}

func TestNativeSnapshotWritePublishesEpochBeforeAcknowledgement(t *testing.T) {
	c, o := instrumentController(t)
	records := callRecords(t)
	c.LaunchInstrumented = func(_ context.Context, _ native.LaunchOptions, out io.Writer) error {
		r := records[0]
		if err := instrumentation.WriteRecord(out, r); err != nil {
			return err
		}
		r.Sequence++
		r.At++
		r.Metrics.Epoch++
		if err := instrumentation.WriteRecord(out, r); err != nil {
			return err
		}
		_, retained := c.MetricView()
		if retained == nil || retained.Metrics.Epoch != 1 {
			return errors.New("native writer returned before service retained Clear")
		}
		return nil
	}
	if err := c.StartInstrumented(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	awaitInstrumentedStop(t, c)
	if c.Status().State != "stopped" {
		t.Fatal(c.Status())
	}
}
func TestClearRejectsInactiveAndSamplingSessions(t *testing.T) {
	c := New()
	if err := c.Clear(context.Background()); err == nil {
		t.Fatal("Clear accepted idle session")
	}
	if err := c.Start(context.Background(), StartOptions{Demo: true}); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	if err := c.Clear(context.Background()); err == nil {
		t.Fatal("native Clear changed sampling")
	}
}
