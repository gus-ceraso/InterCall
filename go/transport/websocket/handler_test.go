package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	intercall "github.com/cerasos/intercall/go"
)

func TestHandlerNegotiatesAndShutsDown(t *testing.T) {
	serverExport, serverImport, clientExport, clientImport := negotiatedTestBindings(t)
	h := NewHandler(serverExport, serverImport)
	server := httptest.NewServer(h)
	client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), clientExport, clientImport)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Wait()
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.Close()
}

func TestHandlerRejectsInvalidBindings(t *testing.T) {
	h := NewHandler(intercallExportZero(), intercallImportZero())
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestHandlerNilBehavior(t *testing.T) {
	var h *Handler
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("nil handler status = %d", recorder.Code)
	}
	if err := h.Shutdown(context.Background()); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("nil Shutdown() = %v", err)
	}
}

// Helpers keep the invalid-binding test independent of generated packages.
func intercallExportZero() intercall.ExportBinding { return intercall.ExportBinding{} }
func intercallImportZero() intercall.ImportBinding { return intercall.ImportBinding{} }
