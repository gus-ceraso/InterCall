//go:build unix

package unixsocket

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	intercall "github.com/cerasos/intercall/go"
)

func testBindings(t *testing.T) (intercall.ExportBinding, intercall.ImportBinding, intercall.ExportBinding, intercall.ImportBinding) {
	t.Helper()
	var serverExportID, clientExportID intercall.InterfaceID
	serverExportID[0] = 1
	clientExportID[0] = 2
	newExport := func(id intercall.InterfaceID) intercall.ExportBinding {
		export, err := intercall.NewExportBindingWithInterfaceID(func(context.Context, uint64, []byte) (uint64, []byte) {
			return 0, nil
		}, id)
		if err != nil {
			t.Fatal(err)
		}
		return export
	}
	return newExport(serverExportID), intercall.NewImportBindingWithInterfaceID(clientExportID),
		newExport(clientExportID), intercall.NewImportBindingWithInterfaceID(serverExportID)
}

func TestNegotiatedUnixConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverExport, serverImport, clientExport, clientImport := testBindings(t)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptConnection(context.Background(), serverExport, serverImport)
		if err != nil {
			serverDone <- err
			return
		}
		serverDone <- conn.Close()
		_ = conn.Wait()
	}()
	client, err := Dial(context.Background(), path, clientExport, clientImport)
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

func TestNegotiatedUnixValidationBeforeDial(t *testing.T) {
	legacyExport, err := intercall.NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) {
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyImport := intercall.NewImportBinding()
	_, err = Dial(context.Background(), filepath.Join(t.TempDir(), "missing"), legacyExport, legacyImport)
	if !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("Dial() = %v, want invalid argument", err)
	}
}

func TestAcceptConnectionCanceledAfterAccept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := ListenStream(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverExport, serverImport, _, _ := testBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := listener.AcceptConnection(ctx, serverExport, serverImport)
		result <- err
	}()
	clientDone := make(chan error, 1)
	go func() {
		client, err := DialStream(context.Background(), path)
		if err != nil {
			clientDone <- err
			return
		}
		defer client.Close()
		cancel()
		clientDone <- nil
	}()
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("AcceptConnection() = %v, want context.Canceled", err)
	}
}
