package websocket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	intercall "github.com/cerasos/intercall/go"
	coderws "github.com/coder/websocket"
)

func TestAsymmetricEchoServerRejected(t *testing.T) {
	_, _, clientExport, clientImport := negotiatedTestBindings(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, reader, err := conn.Reader(context.Background())
		if err != nil {
			return
		}
		id := make([]byte, 32)
		_, err = io.ReadFull(reader, id)
		if err == nil {
			_ = conn.Write(context.Background(), coderws.MessageBinary, id)
		}
	}))
	defer server.Close()
	_, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), clientExport, clientImport)
	if !errors.Is(err, intercall.ErrInterfaceMismatch) {
		t.Fatalf("Dial() = %v, want interface mismatch", err)
	}
}

func TestPassiveEchoClientCannotStartServerHandshake(t *testing.T) {
	serverExport, serverImport, _, _ := negotiatedTestBindings(t)
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := AcceptStream(context.Background(), w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err = intercall.NewNegotiatedServerConnection(ctx, stream, serverExport, serverImport)
		serverDone <- err
		_ = stream.Close()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := <-serverDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("server negotiation = %v", err)
	}
}

func TestRepeatedWebSocketConnectShutdown(t *testing.T) {
	serverExport, serverImport, clientExport, clientImport := negotiatedTestBindings(t)
	for i := 0; i < 10; i++ {
		h := NewHandler(serverExport, serverImport)
		server := httptest.NewServer(h)
		client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), clientExport, clientImport)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		if err := h.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
		_ = client.Wait()
		server.Close()
	}
}
