//go:build unix

package unixsocket

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

func TestDialStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	client, err := DialStream(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case server := <-accepted:
		defer server.Close()
	case err := <-acceptErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for accept")
	}
}

func TestDialStreamValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		path string
		want error
	}{
		{"nil context", nil, "/tmp/intercall-test", intercall.ErrInvalidArgument},
		{"empty path", context.Background(), "", intercall.ErrInvalidArgument},
		{"NUL path", context.Background(), "a\x00b", intercall.ErrInvalidArgument},
		{"abstract path", context.Background(), "@intercall", intercall.ErrInvalidArgument},
		{"canceled", ctx, filepath.Join(t.TempDir(), "missing"), context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialStream(tc.ctx, tc.path)
			if err == nil || !errors.Is(err, tc.want) {
				t.Fatalf("DialStream() error = %v, want errors.Is(..., %v)", err, tc.want)
			}
		})
	}
}
