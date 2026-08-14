package websocket_test

import (
	"context"
	"net/http"

	intercall "github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/transport/websocket"
)

func ExampleDial() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = websocket.Dial(ctx, "wss://hello.example.com/intercall", intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
}

func ExampleListenAndServe() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = websocket.ListenAndServe(ctx, "127.0.0.1:8080", "/intercall", intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
}

func ExampleNewHandler() {
	h := websocket.NewHandler(intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
	mux := http.NewServeMux()
	mux.Handle("/intercall", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authentication middleware runs before the WebSocket upgrade.
		h.ServeHTTP(w, r)
	}))
	_ = mux
}

func ExampleDialStream() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _ = websocket.DialStream(ctx, "wss://hello.example.com/intercall", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer token"}},
	})
}

func Example_authenticatedServer() {
	if false {
		var w http.ResponseWriter
		var r *http.Request
		stream, _ := websocket.AcceptStream(context.Background(), w, r, nil)
		_, _ = intercall.NewNegotiatedServerConnection(context.Background(), stream,
			intercall.EmptyExportBinding(), intercall.EmptyImportBinding())
	}
}
