package native

import (
	"errors"
	"time"
)

type LabOptions struct {
	Workload                      string
	Threads, Iterations, Capacity int
	Timeout                       time.Duration
	InstallHooks                  bool
}

func (o LabOptions) validate() error {
	if o.Workload == "" || o.Threads < 1 || o.Threads > 16 || o.Iterations < 1 || o.Iterations > 100000 || o.Capacity < 1 || o.Capacity > 1024 || o.Timeout < time.Millisecond || o.Timeout > time.Minute {
		return errors.New("invalid controlled native workload options")
	}
	return nil
}
