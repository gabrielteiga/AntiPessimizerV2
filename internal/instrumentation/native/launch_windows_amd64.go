package native

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"golang.org/x/sys/windows"
)

// Launch creates and exclusively owns a fresh process. It stops at the original
// entry point, loads the native adapter and installs checked leaf hooks before
// resuming application startup. It never attaches to an unrelated process.
func Launch(parent context.Context, o LaunchOptions, out io.Writer) (resultErr error) {
	if o.Control != nil {
		if err := o.Control.claim(); err != nil {
			return err
		}
		defer close(o.Control.done)
	}
	if err := o.validate(); err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	path, err := filepath.Abs(o.Image)
	if err != nil {
		return err
	}
	dll, err := filepath.Abs(o.DLL)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 128<<20 {
		return errors.New("invalid instrumented executable")
	}
	image, err := pe.NewFile(f)
	if err != nil {
		return err
	}
	defer image.Close()
	header, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || header.AddressOfEntryPoint == 0 || header.AddressOfEntryPoint >= header.SizeOfImage {
		return errors.New("instrumented target must be a Windows x64 executable")
	}
	data := make([]byte, int(stat.Size()))
	if _, err := f.ReadAt(data, 0); err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	build := "sha256:" + hex.EncodeToString(hash[:])
	if o.ImageID != "" && o.ImageID != build {
		return errors.New("executable changed after symbol selection")
	}
	checks, err := CheckImageProcedures(data, o.Procedures)
	if err != nil {
		return err
	}
	if err := RequireSupported(checks); err != nil {
		return err
	}
	// Resolve short names/junctions before matching the debugger's file path.
	// Windows temporary folders may use RUNNER~1 while debug events report
	// the long name, especially for Unicode/spaced fixture paths.
	dll, err = canonicalLaunchFile(dll)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	session := "instrumented-" + hex.EncodeToString(nonce[:])
	address := `\\.\pipe\AntiPessimizer-Launch-` + hex.EncodeToString(nonce[:])
	listener, err := transport.Listen(address)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(parent, o.Timeout)
	defer cancel()
	var commandsDone chan struct{}
	defer func() {
		cancel()
		if commandsDone != nil {
			<-commandsDone
		}
	}()
	clearAck := make(chan error, 1)
	selected := make([]debugSelection, len(o.Procedures))
	for i, p := range o.Procedures {
		selected[i] = debugSelection{p.ID, p.RVA, p.Size}
	}
	d, err := launchStopped(ctx, path, o.Args, dll, address, header.AddressOfEntryPoint, selected, uint32(o.Capacity))
	if err != nil {
		return err
	}
	defer func() {
		cancel()
		windows.TerminateJobObject(d.job, 1)
		<-d.done
		if result, err := windows.WaitForSingleObject(d.process.Process, 5000); err != nil || result != windows.WAIT_OBJECT_0 {
			resultErr = errors.Join(resultErr, fmt.Errorf("owned instrumented process did not terminate: wait=%d error=%v", result, err))
		}
		if resultErr != nil && d.err != nil && !errors.Is(d.err, context.Canceled) {
			resultErr = errors.Join(resultErr, d.err)
		}
		windows.CloseHandle(d.job)
		windows.CloseHandle(d.process.Process)
	}()
	go func() { <-d.done; listener.Close() }()
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { windows.TerminateJobObject(d.job, 1); listener.Close(); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	defer func() {
		if ctx.Err() != nil {
			resultErr = errors.Join(ctx.Err(), resultErr)
		}
	}()
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(d.process.Process, &created, &exit, &kernel, &user); err != nil {
		return err
	}
	target := profiling.Target{PID: d.process.ProcessId, Image: filepath.Base(path), Start: time.Unix(0, created.Nanoseconds()).UTC()}
	conn, err := listener.Accept()
	if err != nil {
		<-d.done
		return fmt.Errorf("instrumented startup connection: %w", err)
	}
	defer conn.Close()
	stoppedConn := make(chan struct{})
	stopConn := context.AfterFunc(ctx, func() { conn.Close(); close(stoppedConn) })
	defer func() {
		if !stopConn() {
			<-stoppedConn
		}
	}()
	reader, err := NewReader(conn, d.process.ProcessId)
	if err != nil {
		return err
	}
	if reader.Header.ImageSize != header.SizeOfImage || len(reader.Header.Procedures) != len(selected) {
		return errors.New("instrumented native catalog mismatch")
	}
	var procedures []instrumentation.Procedure
	for _, actual := range reader.Header.Procedures {
		found := false
		for _, p := range o.Procedures {
			if p.ID == actual.ID && p.RVA == actual.RVA {
				found = true
				procedures = append(procedures, instrumentation.Procedure{ID: p.ID, Location: profiling.Location{ModuleID: build, Module: target.Image, Procedure: p.Name, Unit: p.Unit, RVA: fmt.Sprintf("0x%016x", p.RVA), Status: "resolved"}})
			}
		}
		if !found {
			return errors.New("instrumented selection changed in target")
		}
	}
	startup := func() error {
		if err := installHooks(conn, data, reader.Header.Procedures); err != nil {
			return err
		}
		if reader.Header.Version < 4 {
			return errors.New("update the native adapter DLL: owned thread lifecycles require wire v4")
		}
		commandsDone = make(chan struct{})
		go func() {
			defer close(commandsDone)
			runCommands(ctx, conn, o.Control, clearAck, cancel, d.exitedThreads)
		}()
		return nil
	}
	awaitExit := func(ctx context.Context) error {
		select {
		case <-d.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if d.err != nil {
			return d.err
		}
		if d.exit != 0 {
			return fmt.Errorf("instrumented application exited with code %d", d.exit)
		}
		return nil
	}
	return consumeCaptureControlled(ctx, reader, target, session, procedures, out, startup, awaitExit, clearAck)
}

func canonicalLaunchFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]uint16, 1024)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &b[0], uint32(len(b)), 0)
	if err != nil {
		return "", err
	}
	if n >= uint32(len(b)) {
		if n > 32768 {
			return "", errors.New("native adapter path exceeds Windows limit")
		}
		b = make([]uint16, n+1)
		n, err = windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &b[0], uint32(len(b)), 0)
		if err != nil || n >= uint32(len(b)) {
			return "", fmt.Errorf("resolve native adapter path: %w", err)
		}
	}
	return windows.UTF16ToString(b[:n]), nil
}
