package platform

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestKernelLifecycle(t *testing.T) {
	now := time.Now()
	s := KernelState{PID: 42, Threads: map[uint32]time.Time{7: now}, Modules: []Module{{Base: 0x5000, Size: 100, Path: "lab.exe"}}}
	profile := make([]byte, 16)
	binary.LittleEndian.PutUint64(profile, 0x5020)
	binary.LittleEndian.PutUint32(profile[8:], 7)
	sample, ok := s.Feed("profile", 46, 2, now, profile)
	if !ok || sample.Module.Path != "lab.exe" {
		t.Fatal(sample, ok)
	}
	thread := make([]byte, 8)
	binary.LittleEndian.PutUint32(thread, 99)
	binary.LittleEndian.PutUint32(thread[4:], 7)
	s.Feed("thread", 1, 3, now, thread)
	if _, ok = s.Feed("profile", 46, 2, now, profile); ok {
		t.Fatal("reused TID attributed to target")
	}
	binary.LittleEndian.PutUint32(thread, 42)
	s.Feed("thread", 1, 3, now.Add(time.Second), thread)
	if _, ok = s.Feed("profile", 46, 2, now, profile); ok {
		t.Fatal("sample predates thread")
	}
	image := make([]byte, 56)
	binary.LittleEndian.PutUint64(image, 0x5000)
	binary.LittleEndian.PutUint32(image[16:], 42)
	s.Feed("image", 2, 2, now, image)
	sample, ok = s.Feed("profile", 46, 2, now.Add(time.Second), profile)
	if !ok || sample.Module.Size != 0 {
		t.Fatal("unloaded image reused", sample)
	}
}
func FuzzKernelDecode(f *testing.F) {
	f.Add([]byte{1, 2, 3}, uint8(46), uint8(2))
	f.Fuzz(func(t *testing.T, b []byte, op, v uint8) {
		s := KernelState{PID: 1, Threads: map[uint32]time.Time{}}
		for _, kind := range []string{"thread", "image", "profile"} {
			s.Feed(kind, op, v, time.Now(), b)
		}
	})
}

func TestDelayedEventsDoNotUseNewModuleOrDeleteNewThread(t *testing.T) {
	now := time.Now()
	s := KernelState{PID: 42, Threads: map[uint32]time.Time{7: now}, Modules: []Module{{Base: 0x5000, Size: 100, Path: "new.dll", Loaded: now.Add(time.Second)}}}
	profile := make([]byte, 16)
	binary.LittleEndian.PutUint64(profile, 0x5020)
	binary.LittleEndian.PutUint32(profile[8:], 7)
	sample, ok := s.Feed("profile", 46, 2, now, profile)
	if !ok || sample.Module.Path != "" {
		t.Fatal("old IP attributed to new module", sample)
	}
	// An old thread-end record must not erase the later instance with this TID.
	thread := make([]byte, 8)
	binary.LittleEndian.PutUint32(thread, 42)
	binary.LittleEndian.PutUint32(thread[4:], 7)
	s.Threads[7] = now.Add(time.Second)
	s.Feed("thread", 2, 3, now, thread)
	if _, ok := s.Threads[7]; !ok {
		t.Fatal("old lifecycle deleted later thread")
	}
}
