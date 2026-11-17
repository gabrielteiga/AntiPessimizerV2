//go:build !windows || !amd64

package native

import (
	"context"
	"errors"
	"io"
)

func RunLab(ctx context.Context, options LabOptions, out io.Writer) error {
	if err := options.validate(); err != nil {
		return err
	}
	return errors.New("controlled native capture requires Windows x64")
}
