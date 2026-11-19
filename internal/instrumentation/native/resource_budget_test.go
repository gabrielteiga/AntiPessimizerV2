package native

import "testing"

// Go 1.27 runtime/os_windows.go mpreinit/minit creates two events, a self
// thread handle, two high-resolution timers and one wait-completion packet
// for each new M. Accept only this exact measured vector, with matching Go
// creation and OS-thread counts. Never excuse a target/file/debugger handle.
func exactRuntimeHandleBundle(warm, current map[string]int, createdThreads, osThreads, handles int) bool {
	if createdThreads < 1 || createdThreads > 8 || createdThreads != osThreads || handles != 6*createdThreads {
		return false
	}
	want := map[string]int{"Thread (self)": createdThreads, "Event": 2 * createdThreads, "IRTimer": 2 * createdThreads, "WaitCompletionPacket": createdThreads}
	for name, n := range current {
		if n-warm[name] != want[name] {
			return false
		}
	}
	for name, n := range warm {
		if current[name]-n != want[name] {
			return false
		}
	}
	for name, n := range want {
		if current[name]-warm[name] != n {
			return false
		}
	}
	return current["DebugObject"] == 0
}

func TestResourceBudgetRecognizesOnlyRecordedGoRuntimeHandleGrowth(t *testing.T) {
	warm := map[string]int{"Thread (self)": 10, "Event": 26, "IRTimer": 22, "WaitCompletionPacket": 16, "File": 7, "Process": 0}
	observed := map[string]int{"Thread (self)": 12, "Event": 30, "IRTimer": 26, "WaitCompletionPacket": 18, "File": 7, "Process": 0}
	if !exactRuntimeHandleBundle(warm, observed, 2, 2, 12) {
		t.Fatal("recorded runtime growth misidentified as owned-capture leak")
	}
	for _, name := range []string{"DebugObject", "Process", "File", "Thread (other process)", "Event", "IRTimer", "WaitCompletionPacket", "Thread (self)"} {
		changed := map[string]int{}
		for k, v := range observed {
			changed[k] = v
		}
		changed[name]++
		if exactRuntimeHandleBundle(warm, changed, 2, 2, 13) || exactRuntimeHandleBundle(warm, changed, 2, 2, 12) {
			t.Fatal("runtime adjustment hid another handle", name)
		}
	}
	for _, args := range [][3]int{{0, 2, 12}, {2, 0, 12}, {1, 2, 12}, {2, 2, 11}, {2, 2, 13}, {9, 9, 54}} {
		if exactRuntimeHandleBundle(warm, observed, args[0], args[1], args[2]) {
			t.Fatal("unproven runtime growth accepted", args)
		}
	}
}
