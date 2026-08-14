package integration

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	intercall "github.com/cerasos/intercall/go"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eexport"
	"github.com/cerasos/intercall/go/internal/integration/fixtures/e2eimport"
	"github.com/cerasos/intercall/go/transport/unixsocket"
	ws "github.com/cerasos/intercall/go/transport/websocket"
)

func TestGeneratedEchoOverUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "echo.sock")
	listener, err := unixsocket.ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptConnection(context.Background(), e2eexport.ExportBinding(), intercall.EmptyImportBinding())
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- conn.Wait()
	}()
	client, err := unixsocket.Dial(context.Background(), path, intercall.EmptyExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2eimport.Echo(bind(client), "unix")
	if err != nil || got != "unix" {
		t.Fatalf("Echo() = %q, %v", got, err)
	}
	_ = client.Close()
	_ = client.Wait()
	if err := <-serverDone; err == nil {
		t.Fatal("server Wait() returned nil")
	}
}

func TestGeneratedEchoOverWebSocket(t *testing.T) {
	h := ws.NewHandler(e2eexport.ExportBinding(), intercall.EmptyImportBinding())
	server := httptest.NewServer(h)
	defer server.Close()
	client, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), intercall.EmptyExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2eimport.Echo(bind(client), "websocket")
	if err != nil || got != "websocket" {
		t.Fatalf("Echo() = %q, %v", got, err)
	}
	_ = client.Close()
	_ = client.Wait()
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTransportMismatchAndRawCompatibility(t *testing.T) {
	var wrong intercall.InterfaceID
	wrong[0] = 0xff
	wrongImport := intercall.NewImportBindingWithInterfaceID(wrong)
	streamA, streamB := newDuplex()
	result := make(chan error, 1)
	go func() {
		_, err := intercall.NewNegotiatedServerConnection(context.Background(), streamA, e2eexport.ExportBinding(), wrongImport)
		result <- err
	}()
	if _, err := streamB.Write(wrong[:]); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, intercall.ErrInterfaceMismatch) {
		t.Fatalf("mismatch error = %v", err)
	}
	_ = streamB.Close()

	a, b := newDuplex()
	left, err := intercall.NewConnection(context.Background(), a, e2eexport.ExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	right, err := intercall.NewConnection(context.Background(), b, e2eexport.ExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2eimport.Echo(bind(right), "raw")
	if err != nil || got != "raw" {
		t.Fatalf("raw Echo() = %q, %v", got, err)
	}
	_ = left.Close()
	_ = right.Close()
	_ = left.Wait()
	_ = right.Wait()
}
