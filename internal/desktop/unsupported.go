//go:build !windows || !amd64

package desktop

import "errors"

func Run(address string) error {
	return errors.New("native desktop viewer requires Windows amd64; terminal and archive modes are portable")
}

func RunArchive(address, path string) error { return Run(address) }
