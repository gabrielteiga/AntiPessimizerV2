//go:build !windows || !amd64

package native

import (
	"context"
	"errors"
	"io"
)

func Launch(ctx context.Context, o LaunchOptions, out io.Writer) error {
	if err := o.validate(); err != nil {
		return err
	}
	return errors.New("instrumented application launch requires Windows x64")
}
