package transport

import (
	"context"
	"fmt"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
	"strings"
)

func DefaultAddress() string { return `\\.\pipe\AntiPessimizerV2` }
func local(address string) bool {
	return strings.HasPrefix(address, `\\.\pipe\`) && !strings.Contains(address[9:], `\`)
}
func Listen(address string) (net.Listener, error) {
	if !local(address) {
		return nil, fmt.Errorf("only local named pipes are allowed")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(address, &winio.PipeConfig{SecurityDescriptor: "D:P(D;;GA;;;NU)(A;;GA;;;" + user.User.Sid.String() + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
}
func Dial(ctx context.Context, address string) (net.Conn, error) {
	if !local(address) {
		return nil, fmt.Errorf("only local named pipes are allowed")
	}
	return winio.DialPipeContext(ctx, address)
}
