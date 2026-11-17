//go:build !windows || !amd64

package native

import (
	"context"
	"errors"
	"io"
)

func Launch(ctx context.Context, o LaunchOptions, out io.Writer) error {
	if o.Control != nil {
		if err := o.Control.claim(); err != nil {
			return err
		}
		defer close(o.Control.done)
	}
	if err := o.validate(); err != nil {
		return err
	}
	return errors.New("instrumented application launch requires Windows x64")
}
