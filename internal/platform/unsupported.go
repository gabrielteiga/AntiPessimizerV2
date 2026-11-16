//go:build !windows || !amd64

package platform

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("live sampling requires Windows amd64")

func Processes() ([]Process, error)                                    { return nil, errUnsupported }
func Inspect(pid uint32) (Process, error)                              { return Process{}, errUnsupported }
func Capture(ctx context.Context, pid uint32, emit func(Sample)) error { return errUnsupported }
func ImagePath(path string) string                                     { return path }
