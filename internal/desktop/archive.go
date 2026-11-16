package desktop

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/report"
)

// Probe only the envelope. Each full reader then enforces its own contract and
// retention limits; an unrecognized protocol must never fall back to sampling.
func readArchive(in io.ReadSeeker) ([]profiling.Snapshot, []instrumentation.Record, error) {
	var envelope struct {
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(io.LimitReader(in, instrumentation.MaxFrameBytes)).Decode(&envelope); err != nil {
		return nil, nil, err
	}
	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	switch envelope.Protocol {
	case instrumentation.MetricsProtocol:
		r, err := instrumentation.ReadMetrics(in)
		return nil, r, err
	case "":
		s, err := report.Read(in)
		return s, nil, err
	case instrumentation.EventProtocol:
		return nil, nil, fmt.Errorf("converta os eventos com instrument-replay antes de abrir as métricas")
	default:
		return nil, nil, fmt.Errorf("protocolo de arquivo desconhecido: %q", envelope.Protocol)
	}
}
