//go:build !windows

package transport

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func DefaultAddress() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("antipessimizer-%d", os.Getuid()), "collector.sock")
}
func Listen(address string) (net.Listener, error) {
	dir := filepath.Dir(address)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("socket directory must be private (0700)")
	}
	l, err := net.Listen("unix", address)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(address, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
func Dial(ctx context.Context, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", address)
}
