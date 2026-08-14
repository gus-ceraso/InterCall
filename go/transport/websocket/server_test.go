package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	intercall "github.com/cerasos/intercall/go"
)

func TestListenAndServeExactPathAndShutdown(t *testing.T) {
	serverExport, serverImport, clientExport, clientImport := negotiatedTestBindings(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: exactPathHandler{path: "/literal/{x}", handler: NewHandler(serverExport, serverImport)}, ReadHeaderTimeout: readHeaderTimeout}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- serve(ctx, listener, server, server.Handler.(exactPathHandler).handler) }()
	url := "ws://" + listener.Addr().String() + "/literal/%7Bx%7D"
	conn, err := Dial(context.Background(), url, clientExport, clientImport)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	_ = conn.Wait()
	cancel()
	if err := <-result; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve() = %v", err)
	}
	if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

func TestExactPathRejectsSuffix(t *testing.T) {
	export, imp, _, _ := negotiatedTestBindings(t)
	h := &http.Server{Handler: exactPathHandler{path: "/exact", handler: NewHandler(export, imp)}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go h.Serve(listener)
	defer h.Close()
	resp, err := http.Get("http://" + listener.Addr().String() + "/exact/suffix")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestListenAndServeValidation(t *testing.T) {
	export, imp, _, _ := negotiatedTestBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ListenAndServe(ctx, "127.0.0.1:0", "/", export, imp); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled = %v", err)
	}
	for _, path := range []string{"", "exact", "/a?b", "/a#b"} {
		if err := ListenAndServe(context.Background(), "127.0.0.1:0", path, export, imp); !errors.Is(err, intercall.ErrInvalidArgument) {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
}
