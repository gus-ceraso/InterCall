package websocket

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

func wsURL(httpURL string) string { return "ws" + strings.TrimPrefix(httpURL, "http") }

func TestDialAndAcceptStream(t *testing.T) {
	accepted := make(chan intercall.ByteStream, 1)
	acceptErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := AcceptStream(context.Background(), w, r, nil)
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- stream
	}))
	defer server.Close()
	client, response, err := DialStream(context.Background(), wsURL(server.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("response = %#v", response)
	}
	defer client.Close()
	var serverStream intercall.ByteStream
	select {
	case serverStream = <-accepted:
	case err := <-acceptErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for accept")
	}
	defer serverStream.Close()
	if _, err := client.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 2)
	if _, err := io.ReadFull(serverStream, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("read %q", got)
	}
}

func TestDialReservedHeadersBeforeNetwork(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	for _, name := range []string{"Connection", "Upgrade", "Sec-WebSocket-Protocol", "sec-websocket-extensions"} {
		t.Run(name, func(t *testing.T) {
			_, _, err := DialStream(context.Background(), wsURL(server.URL), &DialOptions{
				HTTPHeader: http.Header{name: {"x"}},
			})
			if !errors.Is(err, intercall.ErrInvalidArgument) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want zero", got)
	}
}

func TestDialHeaderAndNoSubprotocol(t *testing.T) {
	seenHeader := make(chan string, 1)
	seenProtocol := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader <- r.Header.Get("X-Auth")
		seenProtocol <- r.Header.Get("Sec-WebSocket-Protocol")
		stream, err := AcceptStream(context.Background(), w, r, nil)
		if err == nil {
			stream.Close()
		}
	}))
	defer server.Close()
	stream, _, err := DialStream(context.Background(), wsURL(server.URL), &DialOptions{HTTPHeader: http.Header{"X-Auth": {"token"}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if got := <-seenHeader; got != "token" {
		t.Fatalf("X-Auth = %q", got)
	}
	if got := <-seenProtocol; got != "" {
		t.Fatalf("subprotocol = %q", got)
	}
}

func TestStreamArgumentValidation(t *testing.T) {
	if _, _, err := DialStream(nil, "ws://example.test", nil); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, _, err := DialStream(context.Background(), "http://example.test", nil); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("invalid scheme error = %v", err)
	}
	if _, err := AcceptStream(nil, nil, nil, nil); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("nil accept arguments error = %v", err)
	}
}
