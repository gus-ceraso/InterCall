package websocket

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
)

func websocketPair(t *testing.T) (*coderws.Conn, *coderws.Conn) {
	t.Helper()
	serverReady := make(chan *coderws.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err == nil {
			serverReady <- conn
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	client, _, err := coderws.Dial(ctx, wsURL(server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	return <-serverReady, client
}

func TestWebSocketMessageBoundariesAreTransparent(t *testing.T) {
	serverConn, clientConn := websocketPair(t)
	defer serverConn.CloseNow()
	defer clientConn.CloseNow()
	client, err := newStream(context.Background(), clientConn, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first := []byte("header-and-payload")
	for i := 0; i < len(first); i++ {
		if err := serverConn.Write(context.Background(), coderws.MessageBinary, first[i:i+1]); err != nil {
			t.Fatal(err)
		}
	}
	got := make([]byte, len(first))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(first) {
		t.Fatalf("split read = %q", got)
	}

	second := []byte("frame-oneframe-two")
	if err := serverConn.Write(context.Background(), coderws.MessageBinary, second); err != nil {
		t.Fatal(err)
	}
	got = make([]byte, len(second))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(second) {
		t.Fatalf("combined read = %q", got)
	}
}

func TestWebSocketTextMessageRejected(t *testing.T) {
	serverConn, clientConn := websocketPair(t)
	defer serverConn.CloseNow()
	defer clientConn.CloseNow()
	client, err := newStream(context.Background(), clientConn, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := serverConn.Write(context.Background(), coderws.MessageText, []byte("text")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(make([]byte, 4)); err == nil {
		t.Fatal("text message was accepted")
	}
}

func TestWebSocketMessageLimit(t *testing.T) {
	serverConn, clientConn := websocketPair(t)
	defer serverConn.CloseNow()
	defer clientConn.CloseNow()
	client, err := newStream(context.Background(), clientConn, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := serverConn.Write(context.Background(), coderws.MessageBinary, []byte("five5")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, make([]byte, 5)); err == nil {
		t.Fatal("oversized message was accepted")
	}
}
