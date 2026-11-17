package main

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/service"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
)

func subscribeInstrumented(ctx context.Context, address string, out io.Writer) error {
	conn, err := transport.Dial(ctx, address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err = transport.Write(conn, service.Request{Version: service.Version, Op: "instrument-subscribe"}); err != nil {
		return err
	}
	var previous *instrumentation.Record
	for {
		var response service.Response
		if err = transport.Read(conn, &response); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if response.Version != service.Version || response.Error != "" || response.Metric == nil {
			return errors.New("invalid instrumentation subscription response")
		}
		record := response.Metric
		rows := []instrumentation.Record{*record}
		if previous != nil {
			rows = []instrumentation.Record{*previous, *record}
		}
		if err = instrumentation.ValidateRecords(rows); err != nil {
			return err
		}
		if err = instrumentation.WriteRecord(out, *record); err != nil {
			return err
		}
		clone := record.Clone()
		previous = &clone
	}
}
