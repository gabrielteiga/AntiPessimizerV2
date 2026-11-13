// Package stream implements a bounded, versioned NDJSON snapshot stream.
package stream

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
)

const MaxFrameBytes = 8 * 1024 * 1024

func Write(w io.Writer, s profiling.Snapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxFrameBytes {
		return errors.New("snapshot exceeds frame limit")
	}
	b = append(b, '\n')
	n, err := w.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

type Reader struct {
	scanner  *bufio.Scanner
	session  string
	sequence uint64
}

func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes)
	return &Reader{scanner: scanner}
}
func (r *Reader) Next() (profiling.Snapshot, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return profiling.Snapshot{}, fmt.Errorf("read snapshot frame: %w", err)
		}
		return profiling.Snapshot{}, io.EOF
	}
	var s profiling.Snapshot
	if err := json.Unmarshal(r.scanner.Bytes(), &s); err != nil {
		return s, err
	}
	if err := s.Validate(); err != nil {
		return s, err
	}
	if r.session != "" && (s.SessionID != r.session || s.Sequence <= r.sequence) {
		return s, errors.New("session changed or sequence regressed")
	}
	r.session = s.SessionID
	r.sequence = s.Sequence
	return s, nil
}
