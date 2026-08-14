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

func negotiatedTestBindings(t *testing.T) (intercall.ExportBinding, intercall.ImportBinding, intercall.ExportBinding, intercall.ImportBinding) {
	t.Helper()
	var serverID, clientID intercall.InterfaceID
	serverID[0] = 1
	clientID[0] = 2
	makeExport := func(id intercall.InterfaceID) intercall.ExportBinding {
		binding, err := intercall.NewExportBindingWithInterfaceID(func(context.Context, uint64, []byte) (uint64, []byte) {
			return 0, nil
		}, id)
		if err != nil {
			t.Fatal(err)
		}
		return binding
	}
	return makeExport(serverID), intercall.NewImportBindingWithInterfaceID(clientID),
		makeExport(clientID), intercall.NewImportBindingWithInterfaceID(serverID)
}

func TestDialNegotiatedConnection(t *testing.T) {
	serverExport, serverImport, clientExport, clientImport := negotiatedTestBindings(t)
	serverDone := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := AcceptStream(context.Background(), w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		conn, err := intercall.NewNegotiatedServerConnection(context.Background(), stream, serverExport, serverImport)
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- conn.Close()
		_ = conn.Wait()
	}))
	defer server.Close()
	client, err := Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), clientExport, clientImport)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Wait()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestDialNegotiatedValidationBeforeHTTP(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	legacyExport, err := intercall.NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), legacyExport, intercall.NewImportBinding())
	if !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("Dial() = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}
