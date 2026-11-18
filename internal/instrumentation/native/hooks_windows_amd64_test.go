package native

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"golang.org/x/sys/windows"
)

func TestNativeProducerInstallsHooksInOwnedProcess(t *testing.T) {
	path := filepath.Join(filepath.Dir(labPath(t)), "APHookLab.exe")
	var out bytes.Buffer
	if err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 3, Iterations: 10, Capacity: 1024, Timeout: 15 * time.Second, InstallHooks: true}, &out); err != nil {
		t.Fatal(err, out.String())
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil {
		t.Fatal(err)
	}
	r := records[len(records)-1]
	if r.Metrics.Incomplete || r.Metrics.OpenCalls != 0 || r.Metrics.AbandonedCalls != 0 || len(r.Metrics.Rows) != 3 {
		t.Fatal("incomplete installed-hook capture", r)
	}
	seen := map[uint32]bool{}
	for _, row := range r.Metrics.Rows {
		if row.Calls != 10 || row.Location.Procedure != "APHookSum" || row.ExclusiveTicks != row.InclusiveTicks || row.ActiveFrames != 0 || row.AverageInclusiveNS != float64(row.InclusiveTicks)*1e9/float64(r.Metrics.Frequency)/10 || seen[row.ThreadID] {
			t.Fatal("wrong installed-hook metrics", row)
		}
		seen[row.ThreadID] = true
	}
	t.Log("Go controller installed native hooks in its owned process: 10 real calls in each of 3 threads; child verified arguments, LastError, RIP data, return values and complete code restoration")
}
func TestNativeProducerInstalledHookOverflowIsIncomplete(t *testing.T) {
	path := filepath.Join(filepath.Dir(labPath(t)), "APHookLab.exe")
	var out bytes.Buffer
	err := RunLab(context.Background(), LabOptions{Workload: path, Threads: 2, Iterations: 10000, Capacity: 1, Timeout: 15 * time.Second, InstallHooks: true}, &out)
	if !errors.Is(err, ErrLoss) {
		t.Fatal("installed-hook overflow hidden", err)
	}
	records, err := instrumentation.ReadMetrics(&out)
	if err != nil || len(records) == 0 {
		t.Fatal(err)
	}
	if !records[len(records)-1].Metrics.Incomplete {
		t.Fatal("lost installed-hook events advertised exact")
	}
}

// Corrupt the expected source in the controller's install request, without
// changing the file or target. The target must reject before applying a patch.
type corruptHookSource struct {
	io.ReadWriter
	written   int
	prefix    [25]byte
	corrupted bool
}

func (w *corruptHookSource) Write(b []byte) (int, error) {
	data := append([]byte(nil), b...)
	if w.written < len(w.prefix) {
		copy(w.prefix[w.written:], data)
	}
	// The payload includes a separately reserved runtime table on current
	// adapters. Locate source bytes from the actual plan header, not a stride.
	expectedSource := len(w.prefix) + int(binary.LittleEndian.Uint32(w.prefix[13:17])) + 8
	flip := w.written+len(data) >= len(w.prefix) && w.written <= expectedSource && w.written+len(data) > expectedSource
	if flip {
		data[expectedSource-w.written] ^= 1
	}
	n, err := w.ReadWriter.Write(data)
	if flip && n > expectedSource-w.written {
		w.corrupted = true
	}
	w.written += n
	return n, err
}
func TestNativeProducerRejectsDifferentOriginalCode(t *testing.T) {
	path := filepath.Join(filepath.Dir(labPath(t)), "APHookLab.exe")
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	address := `\\.\pipe\AntiPessimizer-Hook-Reject-` + hex.EncodeToString(nonce[:])
	listener, err := transport.Listen(address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	p, job, err := createOwned(path, []string{"--pipe", address, "--threads", "1", "--iterations", "1", "--capacity", "1024"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { windows.WaitForSingleObject(p.Process, windows.INFINITE); listener.Close(); close(done) }()
	defer func() {
		windows.TerminateJobObject(job, 1)
		windows.CloseHandle(job)
		<-done
		windows.CloseHandle(p.Process)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { windows.TerminateJobObject(job, 1); listener.Close(); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r, err := NewReader(conn, p.ProcessId)
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &corruptHookSource{ReadWriter: conn}
	err = installHooks(proxy, image, r.Header.Procedures)
	if !proxy.corrupted {
		t.Fatal("source mismatch fixture did not reach the expected bytes", err)
	}
	if err == nil {
		t.Fatal("native installer accepted different original bytes")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.Process, &code); err != nil || code != 4 {
		t.Fatalf("native rejection failed to leave original code intact: exit=%d error=%v", code, err)
	}
}
