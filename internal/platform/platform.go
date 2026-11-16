// Package platform defines OS discovery and sampling inputs, independent of UI.
package platform

import "time"

type Process struct {
	PID   uint32    `json:"pid"`
	Name  string    `json:"name"`
	Start time.Time `json:"start"`
}
type Module struct {
	Loaded     time.Time
	Base, Size uint64
	Path       string
}
type Sample struct {
	At     time.Time
	Thread uint32
	IP     uint64
	Module Module
}
