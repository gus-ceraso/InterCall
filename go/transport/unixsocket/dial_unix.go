//go:build unix

package unixsocket

import (
	"context"
	"fmt"
	"net"

	intercall "github.com/cerasos/intercall/go"
)

// DialStream dials path as a Unix stream socket. The context is used for the
// dialing operation; after success, the returned connection is owned by the
// caller and its lifetime is controlled by Close.
func DialStream(ctx context.Context, path string) (*net.UnixConn, error) {
	if ctx == nil {
		return nil, fmt.Errorf("unixsocket: nil dial context: %w", intercall.ErrInvalidArgument)
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("unixsocket: dial %q: %w", path, err)
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("unixsocket: dial returned %T, want *net.UnixConn", conn)
	}
	return unixConn, nil
}
