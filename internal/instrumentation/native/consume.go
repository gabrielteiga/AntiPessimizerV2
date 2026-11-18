package native

import (
	"context"
	"errors"
	"fmt"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"io"
	"time"
)

// consumeCapture owns the engine for both cooperative tests and debug-launched
// targets. Every failure preserves completed rows and explicitly invalidates
// completeness; a verified final batch and target exit are required for success.
func consumeCapture(ctx context.Context, reader *Reader, target profiling.Target, session string, procedures []instrumentation.Procedure, out io.Writer, startup func() error, awaitExit func(context.Context) error) error {
	return consumeCaptureControlled(ctx, reader, target, session, procedures, out, startup, awaitExit, nil)
}
func consumeCaptureControlled(ctx context.Context, reader *Reader, target profiling.Target, session string, procedures []instrumentation.Procedure, out io.Writer, startup func() error, awaitExit func(context.Context) error, clearAck chan<- error) (resultErr error) {
	engine, err := instrumentation.New(instrumentation.Config{Frequency: reader.Header.Frequency, MaxThreads: 64, MaxDepth: 1024, MaxRows: 4096}, procedures)
	if err != nil {
		return err
	}
	var sequence uint64
	writeFailed := false
	emit := func(at uint64) error {
		s, err := engine.Snapshot()
		if err != nil {
			return err
		}
		sequence++
		r := instrumentation.Record{Protocol: instrumentation.MetricsProtocol, Version: 2, Selection: append([]instrumentation.Procedure(nil), procedures...), SessionID: session, Target: target, Source: "native-hook", Clock: "qpc", Sequence: sequence, At: at, Metrics: s}
		if err := instrumentation.WriteRecord(out, r); err != nil {
			writeFailed = true
			return err
		}
		return nil
	}
	defer func() {
		if resultErr != nil && !writeFailed {
			resultErr = errors.Join(resultErr, engine.MarkLoss(), emit(reader.LatestTick()))
		}
	}()
	if err := startup(); err != nil {
		return err
	}
	active := map[uint32]bool{}
	lastPublish := time.Now()
	for {
		e, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch e.Type {
		case "clear":
			err = engine.Clear(e.Tick)
		case "heartbeat":
			// Publish liveness without closing calls or changing accounting time.
		case "thread_start":
			err = engine.StartThread(e.Thread, e.Tick)
			if err == nil {
				active[e.Thread] = true
			}
		case "enter":
			err = engine.Enter(e.Thread, e.Procedure, e.Tick)
		case "exit":
			err = engine.Exit(e.Thread, e.Procedure, e.Tick)
		case "unwind_exit":
			err = engine.ExitUnwound(e.Thread, e.Procedure, e.Tick)
		case "unwind":
			err = engine.Unwind(e.Thread, e.KeepDepth, e.Tick)
		case "thread_end":
			err = engine.EndThread(e.Thread, e.Tick)
			if err == nil {
				delete(active, e.Thread)
			}
		}
		if err != nil {
			return fmt.Errorf("native %s: %w", e.Type, err)
		}
		if e.Type == "clear" || time.Since(lastPublish) >= 250*time.Millisecond {
			if err := emit(reader.LatestTick()); err != nil {
				return err
			}
			lastPublish = time.Now()
		}
		if e.Type == "clear" {
			select {
			case clearAck <- nil:
			default:
			}
		}
	}
	if len(active) != 0 {
		return errors.New("native capture completed without thread termination events")
	}
	if err := awaitExit(ctx); err != nil {
		return err
	}
	return emit(reader.FinalTick())
}
