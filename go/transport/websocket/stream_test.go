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

func TestStreamBinaryRoundTrip(t *testing.T) {
	serverReady := make(chan *coderws.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		serverReady <- conn
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	clientConn, _, err := coderws.Dial(ctx, "ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	serverConn := <-serverReady
	client, err := newStream(ctx, clientConn, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverStream, err := newStream(ctx, serverConn, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer serverStream.Close()
	want := []byte("hello")
	if _, err := serverStream.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("read %q, want %q", got, want)
	}
}

func TestNormalizeMessageLimit(t *testing.T) {
	if got, _ := normalizeMessageLimit(0); got != DefaultMessageLimit {
		t.Fatalf("default limit = %d", got)
	}
	if got, _ := normalizeMessageLimit(-1); got != -1 {
		t.Fatalf("unlimited limit = %d", got)
	}
	if _, err := normalizeMessageLimit(-2); err == nil {
		t.Fatal("negative limit accepted")
	}
}

func TestStreamCloseUnblocksRead(t *testing.T) {
	serverReady := make(chan *coderws.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err == nil {
			serverReady <- conn
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+server.URL[len("http"):], nil)
	if err != nil {
		t.Fatal(err)
	}
	<-serverReady
	stream, err := newStream(ctx, conn, 0)
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := stream.Read(make([]byte, 1))
		readDone <- err
	}()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("blocked read survived Close")
	}
}
