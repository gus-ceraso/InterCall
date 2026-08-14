package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestHandlerShutdownRejectsLaterUpgrade(t *testing.T) {
	serverExport, serverImport, _, _ := negotiatedTestBindings(t)
	h := NewHandler(serverExport, serverImport)
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestHandlerShutdownClosesActiveConnection(t *testing.T) {
	serverExport, serverImport, clientExport, clientImport := negotiatedTestBindings(t)
	h := NewHandler(serverExport, serverImport)
	server := httptest.NewServer(h)
	defer server.Close()
	client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), clientExport, clientImport)
	if err != nil {
		t.Fatal(err)
	}
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- h.Shutdown(context.Background()) }()
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	if err := client.Wait(); err == nil {
		t.Fatal("client Wait() returned nil")
	}
}

func TestHandlerShutdownContextAndConcurrentCalls(t *testing.T) {
	serverExport, serverImport, _, _ := negotiatedTestBindings(t)
	h := NewHandler(serverExport, serverImport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown(canceled) = %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Shutdown(context.Background())
		}()
	}
	wg.Wait()
}
