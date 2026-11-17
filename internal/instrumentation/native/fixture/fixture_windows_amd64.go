// Package fixture exposes private Win64 assembly used only by native bridge tests.
// These leaf procedures use the Windows ABI and never call Go from native code.
package fixture

import "sync/atomic"

var entryProbe, exitProbe uintptr

//go:noescape
func Addresses() (entry, body uintptr)

// SetProbes requires all fixture calls to be quiescent.
func SetProbes(entry, exit uintptr) {
	atomic.StoreUintptr(&entryProbe, entry)
	atomic.StoreUintptr(&exitProbe, exit)
}
