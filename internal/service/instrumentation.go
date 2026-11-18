package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation/native"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

// InstrumentOptions always launches a NEW owned process. Stop cancels that
// process, never attaches to or terminates an unrelated running application.
type InstrumentOptions struct {
	CatalogID  string   `json:"catalog_id"`
	Image      string   `json:"image"`
	ImageID    string   `json:"image_id"`
	Procedures []uint32 `json:"procedure_ids"`
	Args       []string `json:"args,omitempty"`
	TimeoutMS  int64    `json:"timeout_ms,omitempty"`
}

type CatalogQuery struct {
	Check  bool   `json:"check,omitempty"`
	Image  string `json:"image"`
	Text   string `json:"text,omitempty"`
	Unit   string `json:"unit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type CatalogPage struct {
	Aliases      []native.ProcedureAlias    `json:"aliases,omitempty"`
	AliasTotal   int                        `json:"alias_total,omitempty"`
	Checks       []native.ProcedureCheck    `json:"checks,omitempty"`
	CatalogID    string                     `json:"catalog_id"`
	ImageID      string                     `json:"image_id"`
	SymbolSource string                     `json:"symbol_source"`
	Total        int                        `json:"total"`
	Procedures   []native.SelectedProcedure `json:"procedures"`
}

func (c *Controller) StartInstrumented(parent context.Context, o InstrumentOptions) error {
	if o.TimeoutMS == 0 {
		o.TimeoutMS = 60000
	}
	if o.TimeoutMS < 1 || o.TimeoutMS > 86400000 || len(o.Args) > 256 {
		return errors.New("invalid instrumented timeout/arguments")
	}
	for _, arg := range o.Args {
		if len(arg) > 32768 {
			return errors.New("instrumented argument too long")
		}
	}
	catalog, err := c.LoadCatalog(o.Image)
	if err != nil {
		return err
	}
	if o.CatalogID == "" || o.CatalogID != catalog.CatalogID {
		return errors.New("symbol catalog changed; reload before selecting procedures")
	}
	if err = catalog.RequireImage(o.ImageID); err != nil {
		return err
	}
	selected, err := catalog.Select(o.Procedures)
	if err != nil {
		return err
	}
	checks, err := c.CheckCatalog(catalog, selected)
	if err != nil {
		return err
	}
	if err := native.RequireSupported(checks); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	options := native.LaunchOptions{Image: o.Image, ImageID: catalog.ImageID, DLL: filepath.Join(filepath.Dir(executable), "AntiPessimizerNative.dll"), Args: append([]string(nil), o.Args...), Procedures: selected, Capacity: 1024, Timeout: time.Duration(o.TimeoutMS) * time.Millisecond}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	sessionID := "instrumented-" + hex.EncodeToString(id[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("a session is already active")
	}
	c.latest = nil
	c.store = nil
	c.metricHistory = nil
	c.metricSizes = nil
	c.metricRows = 0
	c.metricBytes = 0
	c.state = Status{State: "starting", Mode: "instrumentation", SessionID: sessionID, Target: profiling.Target{Image: filepath.Base(o.Image)}}
	ctx, cancel := context.WithCancel(parent)
	control := native.NewControl()
	c.nativeControl = control
	options.Control = control
	c.cancel = cancel
	c.done = make(chan struct{})
	done := c.done
	launch := c.LaunchInstrumented
	go func() {
		err := c.runInstrumented(ctx, options, sessionID, launch)
		c.mu.Lock()
		c.cancel = nil
		c.nativeControl = nil
		c.state.State = "stopped"
		if err != nil && !errors.Is(err, context.Canceled) {
			c.state.State = "error"
			c.state.Error = err.Error()
		}
		close(done)
		c.mu.Unlock()
	}()
	return nil
}

// WriteRecord emits exactly one bounded NDJSON frame per Write. Validate that
// frame and stream continuity synchronously: a native Clear acknowledgement can
// only return after the corresponding epoch is retained by this service owner.
type captureSink struct {
	controller *Controller
	sessionID  string
	validator  instrumentation.MetricValidator
}

func (s *captureSink) Write(b []byte) (int, error) {
	if len(b) == 0 || len(b) > instrumentation.MaxFrameBytes || b[len(b)-1] != '\n' || bytes.Count(b, []byte{'\n'}) != 1 {
		return 0, errors.New("invalid native snapshot write boundary")
	}
	reader := instrumentation.NewMetricsReader(bytes.NewReader(b))
	record, err := reader.Next()
	if err != nil {
		return 0, err
	}
	if err = s.validator.Accept(record); err != nil {
		return 0, err
	}
	record.SessionID = s.sessionID
	if err = s.controller.publishMetrics(record); err != nil {
		return 0, err
	}
	return len(b), nil
}
func (c *Controller) runInstrumented(ctx context.Context, o native.LaunchOptions, sessionID string, launch func(context.Context, native.LaunchOptions, io.Writer) error) error {
	sink := captureSink{controller: c, sessionID: sessionID}
	err := launch(ctx, o, &sink)
	if err == nil && c.Status().Sequence == 0 {
		return errors.New("instrumented capture returned no snapshots")
	}
	return err
}

func (c *Controller) publishMetrics(r instrumentation.Record) error {
	encoded, err := json.Marshal(r)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := []instrumentation.Record{r}
	if len(c.metricHistory) > 0 {
		previous = []instrumentation.Record{c.metricHistory[len(c.metricHistory)-1], r}
	}
	if err = instrumentation.ValidateRecords(previous); err != nil {
		return err
	}
	c.metricHistory = append(c.metricHistory, r.Clone())
	c.metricSizes = append(c.metricSizes, len(encoded))
	c.metricRows += len(r.Metrics.Rows)
	c.metricBytes += len(encoded)
	for len(c.metricHistory) > 1 && (len(c.metricHistory) > 256 || c.metricRows > 100000 || c.metricBytes > 64<<20) {
		c.metricRows -= len(c.metricHistory[0].Metrics.Rows)
		c.metricBytes -= c.metricSizes[0]
		c.metricHistory[0] = instrumentation.Record{}
		c.metricHistory = c.metricHistory[1:]
		c.metricSizes = c.metricSizes[1:]
	}
	c.state.Sequence = r.Sequence
	c.state.Target = r.Target
	if c.state.State != "stopping" {
		c.state.State = "running"
	}
	for ch := range c.metricSubscribers {
		select {
		case <-ch:
		default:
		}
		ch <- r.Clone()
	}
	return nil
}

func (c *Controller) MetricView() (Status, *instrumentation.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.metricHistory) == 0 {
		return c.state, nil
	}
	r := c.metricHistory[len(c.metricHistory)-1].Clone()
	return c.state, &r
}
func (c *Controller) MetricHistory() []instrumentation.Record {
	c.mu.Lock()
	// Published records are immutable. Copy their headers under the owner lock,
	// then copy row arrays outside it so history clients cannot stall ingestion.
	records := append([]instrumentation.Record(nil), c.metricHistory...)
	c.mu.Unlock()
	result := make([]instrumentation.Record, len(records))
	for i, r := range records {
		result[i] = r.Clone()
	}
	return result
}
func (c *Controller) SubscribeMetrics() (chan instrumentation.Record, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan instrumentation.Record, 1)
	c.metricSubscribers[ch] = struct{}{}
	if len(c.metricHistory) > 0 {
		ch <- c.metricHistory[len(c.metricHistory)-1].Clone()
	}
	return ch, func() { c.mu.Lock(); defer c.mu.Unlock(); delete(c.metricSubscribers, ch) }
}

func (c *Controller) Catalog(q CatalogQuery) (CatalogPage, error) {
	if q.Offset < 0 || q.Limit < 0 || q.Limit > 512 {
		return CatalogPage{}, errors.New("invalid catalog page")
	}
	if q.Limit == 0 {
		q.Limit = 200
	}
	catalog, err := c.LoadCatalog(q.Image)
	if err != nil {
		return CatalogPage{}, err
	}
	page := CatalogPage{CatalogID: catalog.CatalogID, ImageID: catalog.ImageID, SymbolSource: catalog.SymbolSource, Procedures: []native.SelectedProcedure{}}
	for _, p := range catalog.Search(q.Text, q.Unit) {
		if page.Total >= q.Offset && len(page.Procedures) < q.Limit {
			page.Procedures = append(page.Procedures, p)
		}
		page.Total++
	}
	page.Aliases, page.AliasTotal = catalog.AliasesFor(page.Procedures, 512)
	if q.Check {
		page.Checks, err = c.CheckCatalog(catalog, page.Procedures)
		if err != nil {
			return CatalogPage{}, err
		}
	}
	return page, nil
}

func (c *Controller) LiveView() (Status, *profiling.Snapshot, *instrumentation.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sample *profiling.Snapshot
	var metric *instrumentation.Record
	if c.latest != nil {
		v := c.latest.Clone()
		sample = &v
	}
	if len(c.metricHistory) > 0 {
		v := c.metricHistory[len(c.metricHistory)-1].Clone()
		metric = &v
	}
	return c.state, sample, metric
}

// Clear waits for the adapter's all-thread writer barrier and the resulting
// epoch to reach the validated service history. It never resets only the UI.
func (c *Controller) Clear(ctx context.Context) error {
	c.mu.Lock()
	control := c.nativeControl
	active := c.state.Mode == "instrumentation" && c.cancel != nil && c.state.State == "running"
	c.mu.Unlock()
	if !active || control == nil {
		return native.ErrControlClosed
	}
	return control.Clear(ctx)
}
