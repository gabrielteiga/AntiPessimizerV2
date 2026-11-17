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
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/gabrielteiga/AntiPessimizerV2/internal/instrumentation"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/profiling"
	"github.com/gabrielteiga/AntiPessimizerV2/internal/transport"
	"golang.org/x/sys/windows"
)

// createOwned starts the cooperative fixture already assigned to a kill-on-close
// job. JOB_LIST (Windows 10+) avoids a launch/assignment window with an unowned
// process. No handles are inherited. The fixture waits for the pipe handshake.
func createOwned(path string, args []string) (windows.ProcessInformation, windows.Handle, error) {
	var p windows.ProcessInformation
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return p, 0, err
	}
	fail := func(err error) (windows.ProcessInformation, windows.Handle, error) {
		windows.CloseHandle(job)
		return p, 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fail(err)
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fail(err)
	}
	defer attributes.Delete()
	jobs := [1]windows.Handle{job}
	const jobListAttribute = 0x0002000d // documented PROC_THREAD_ATTRIBUTE_JOB_LIST
	if err := attributes.Update(jobListAttribute, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs)); err != nil {
		return fail(err)
	}
	si := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))}, ProcThreadAttributeList: attributes.List()}
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fail(err)
	}
	command := []string{syscall.EscapeArg(path)}
	for _, arg := range args {
		command = append(command, syscall.EscapeArg(arg))
	}
	line, err := windows.UTF16PtrFromString(strings.Join(command, " "))
	if err != nil {
		return fail(err)
	}
	if err := windows.CreateProcess(app, line, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_NO_WINDOW, nil, nil, &si.StartupInfo, &p); err != nil {
		return fail(err)
	}
	windows.CloseHandle(p.Thread)
	p.Thread = 0
	return p, job, nil
}

// RunLab owns a deliberately cooperating C workload. It is not an attach or
// injector for unmodified Delphi applications. Native callbacks enqueue events;
// this single Go owner accounts and publishes cumulative instrumented metrics.
func RunLab(parent context.Context, options LabOptions, out io.Writer) (resultErr error) {
	if err := options.validate(); err != nil {
		return err
	}
	if err := parent.Err(); err != nil {
		return err
	}
	path, err := filepath.Abs(options.Workload)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 128<<20 {
		return errors.New("invalid native workload image")
	}
	image, err := pe.NewFile(file)
	if err != nil {
		return err
	}
	defer image.Close()
	header, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return errors.New("native workload must be a Windows x64 image")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	build := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	session := "native-" + hex.EncodeToString(nonce[:])
	address := `\\.\pipe\AntiPessimizer-Native-` + hex.EncodeToString(nonce[:])
	listener, err := transport.Listen(address)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	p, job, err := createOwned(path, []string{"--pipe", address, "--threads", strconv.Itoa(options.Threads), "--iterations", strconv.Itoa(options.Iterations), "--capacity", strconv.Itoa(options.Capacity)})
	if err != nil {
		return fmt.Errorf("launch controlled workload: %w", err)
	}
	exited := make(chan struct{})
	go func() { windows.WaitForSingleObject(p.Process, windows.INFINITE); listener.Close(); close(exited) }()
	defer func() {
		windows.TerminateJobObject(job, 1)
		<-exited
		windows.CloseHandle(p.Process)
		windows.CloseHandle(job)
	}()
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { windows.TerminateJobObject(job, 1); listener.Close(); close(callbackDone) })
	defer func() {
		if !stop() {
			<-callbackDone
		}
	}()
	defer func() {
		if ctx.Err() != nil {
			resultErr = errors.Join(ctx.Err(), resultErr)
		}
	}()
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(p.Process, &created, &exit, &kernel, &user); err != nil {
		return err
	}
	target := profiling.Target{PID: p.ProcessId, Image: filepath.Base(path), Start: time.Unix(0, created.Nanoseconds()).UTC()}
	conn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("native workload connection: %w", err)
	}
	defer conn.Close()
	connDone := make(chan struct{})
	stopConn := context.AfterFunc(ctx, func() { conn.Close(); close(connDone) })
	defer func() {
		if !stopConn() {
			<-connDone
		}
	}()
	reader, err := NewReader(conn, p.ProcessId)
	if err != nil {
		return err
	}
	if reader.Header.ImageSize != header.SizeOfImage || len(reader.Header.Procedures) != 4 {
		return errors.New("native catalog does not match controlled image")
	}
	names := map[uint32]string{1: "APLabParent", 2: "APLabChild", 3: "APLabRecursive", 4: "APLabFloat"}
	var procedures []instrumentation.Procedure
	for _, proc := range reader.Header.Procedures {
		if names[proc.ID] == "" {
			return errors.New("unknown controlled procedure ID")
		}
		executable := false
		for _, section := range image.Sections {
			if section.Characteristics&0x20000000 != 0 && uint64(proc.RVA) >= uint64(section.VirtualAddress) && uint64(proc.RVA) < uint64(section.VirtualAddress)+uint64(section.VirtualSize) {
				executable = true
			}
		}
		if !executable {
			return errors.New("native procedure RVA is not executable image code")
		}
		procedures = append(procedures, instrumentation.Procedure{ID: proc.ID, Location: profiling.Location{ModuleID: build, Module: target.Image, Unit: "NativeLab", Procedure: names[proc.ID], RVA: fmt.Sprintf("0x%016x", proc.RVA), Status: "resolved"}})
	}
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
		r := instrumentation.Record{Protocol: instrumentation.MetricsProtocol, Version: 1, SessionID: session, Target: target, Source: "native-hook", Clock: "qpc", Sequence: sequence, At: at, Metrics: s}
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
	if n, err := conn.Write([]byte{1}); err != nil || n != 1 {
		return errors.Join(errors.New("native startup acknowledgement failed"), err)
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
		case "thread_start":
			err = engine.StartThread(e.Thread, e.Tick)
			if err == nil {
				active[e.Thread] = true
			}
		case "enter":
			err = engine.Enter(e.Thread, e.Procedure, e.Tick)
		case "exit":
			err = engine.Exit(e.Thread, e.Procedure, e.Tick)
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
		if time.Since(lastPublish) >= 250*time.Millisecond {
			if err := emit(reader.LatestTick()); err != nil {
				return err
			}
			lastPublish = time.Now()
		}
	}
	if len(active) != 0 {
		return errors.New("native capture completed without thread termination events")
	}
	select {
	case <-exited:
	case <-ctx.Done():
		return ctx.Err()
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.Process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("native workload exited with code %d", code)
	}
	return emit(reader.FinalTick())
}
