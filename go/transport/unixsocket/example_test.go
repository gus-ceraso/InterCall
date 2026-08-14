//go:build unix

package unixsocket_test

import (
	"context"

	intercall "github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/transport/unixsocket"
)

func ExampleDial() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = unixsocket.Dial(ctx, "/run/user/1000/hello.sock", intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
}

func ExampleListenAndServe() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = unixsocket.ListenAndServe(ctx, "/run/user/1000/hello.sock", intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
}

func ExampleDialStream() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = unixsocket.DialStream(ctx, "/run/user/1000/hello.sock")
}

func Example_authenticatedAccept() {
	if false {
		listener, _ := unixsocket.ListenStream("/run/user/1000/hello.sock", nil)
		stream, _ := listener.AcceptStream()
		// Authenticate the Unix peer using application policy before setup.
		_, _ = intercall.NewNegotiatedServerConnection(context.Background(), stream,
			intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
	}
}
