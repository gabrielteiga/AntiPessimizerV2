package platform

import (
	"errors"
	"golang.org/x/sys/windows"
	"strings"
	"testing"
	"time"
)

func TestSnapshotRetriesLoaderChurnBeforeReturningOwnedHandle(t *testing.T) {
	calls := 0
	var waits []time.Duration
	handle, err := snapshotWithRetry(windows.TH32CS_SNAPMODULE, 123, func(flags, pid uint32) (windows.Handle, error) {
		calls++
		if flags != windows.TH32CS_SNAPMODULE || pid != 123 {
			t.Fatal("snapshot target changed", flags, pid)
		}
		if calls < 4 {
			return windows.InvalidHandle, windows.ERROR_BAD_LENGTH
		}
		return windows.Handle(71), nil
	}, func(delay time.Duration) { waits = append(waits, delay) })
	if err != nil || handle != 71 || calls != 4 || len(waits) != 3 {
		t.Fatal("loader churn did not recover", handle, err, calls, waits)
	}
	for i, delay := range waits {
		if delay != time.Millisecond<<i {
			t.Fatal("unexpected retry delay", waits)
		}
	}
}

func TestSnapshotRetryIsBoundedAndPreservesWindowsError(t *testing.T) {
	for _, code := range []error{windows.ERROR_BAD_LENGTH, windows.ERROR_ACCESS_DENIED, windows.ERROR_INVALID_PARAMETER} {
		t.Run(code.Error(), func(t *testing.T) {
			calls, pauses := 0, 0
			var elapsed time.Duration
			handle, err := snapshotWithRetry(windows.TH32CS_SNAPMODULE, 123, func(uint32, uint32) (windows.Handle, error) { calls++; return windows.InvalidHandle, code }, func(delay time.Duration) { pauses++; elapsed += delay })
			want := 1
			if errors.Is(code, windows.ERROR_BAD_LENGTH) {
				want = 8
			}
			if handle != windows.InvalidHandle || !errors.Is(err, code) || calls != want || pauses != want-1 || elapsed > 127*time.Millisecond || !strings.Contains(err.Error(), "CreateToolhelp32Snapshot") || !strings.Contains(err.Error(), "pid=123") {
				t.Fatal("incorrect retry limit or error", handle, err, calls, pauses, elapsed)
			}
		})
	}
}

func TestSnapshotSuccessDoesNotWait(t *testing.T) {
	calls := 0
	handle, err := snapshotWithRetry(0, 0, func(uint32, uint32) (windows.Handle, error) { calls++; return 71, nil }, func(time.Duration) { t.Fatal("successful snapshot retried") })
	if handle != 71 || err != nil || calls != 1 {
		t.Fatal(handle, err, calls)
	}
}
