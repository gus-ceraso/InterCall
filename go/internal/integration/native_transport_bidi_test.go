package integration

import (
	"context"
	"net/http"
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

func TestGeneratedBidirectionalCallsOverUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bidi.sock")
	listener, err := unixsocket.ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverResult := make(chan string, 1)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptConnection(context.Background(), e2eexport.ExportBinding(), e2eimport.ImportBinding())
		if err != nil {
			serverDone <- err
			return
		}
		got, err := e2eimport.Echo(bind(conn), "from-server")
		if err != nil {
			serverResult <- err.Error()
		} else {
			serverResult <- got
		}
		serverDone <- conn.Wait()
	}()
	client, err := unixsocket.Dial(context.Background(), path, e2eexport.ExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2eimport.Echo(bind(client), "from-client")
	if err != nil || got != "from-client" {
		t.Fatalf("client Echo() = %q, %v", got, err)
	}
	if got := <-serverResult; got != "from-server" {
		t.Fatalf("server Echo() = %q", got)
	}
	_ = client.Close()
	_ = client.Wait()
	if err := <-serverDone; err == nil {
		t.Fatal("server Wait() returned nil")
	}
}

func TestGeneratedBidirectionalCallsOverWebSocket(t *testing.T) {
	serverResult := make(chan string, 1)
	serverDone := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := ws.AcceptStream(context.Background(), w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		conn, err := intercall.NewNegotiatedServerConnection(context.Background(), stream, e2eexport.ExportBinding(), e2eimport.ImportBinding())
		if err != nil {
			serverDone <- err
			return
		}
		got, err := e2eimport.Echo(bind(conn), "from-server")
		if err != nil {
			serverResult <- err.Error()
		} else {
			serverResult <- got
		}
		serverDone <- conn.Wait()
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), e2eexport.ExportBinding(), e2eimport.ImportBinding())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2eimport.Echo(bind(client), "from-client")
	if err != nil || got != "from-client" {
		t.Fatalf("client Echo() = %q, %v", got, err)
	}
	if got := <-serverResult; got != "from-server" {
		t.Fatalf("server Echo() = %q", got)
	}
	_ = client.Close()
	_ = client.Wait()
	if err := <-serverDone; err == nil {
		t.Fatal("server Wait() returned nil")
	}
}
