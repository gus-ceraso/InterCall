//go:build unix

package unixsocket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q", path)
}

func TestListenAndServeShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	serverExport, serverImport, clientExport, clientImport := testBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- ListenAndServe(ctx, path, serverExport, serverImport)
	}()
	waitForPath(t, path)
	client, err := Dial(context.Background(), path, clientExport, clientImport)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-result; !errors.Is(err, ErrServerClosed) {
		t.Fatalf("ListenAndServe() = %v, want ErrServerClosed", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket path after shutdown: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.Wait()
}

func TestListenAndServeValidation(t *testing.T) {
	serverExport, serverImport, _, _ := testBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "socket")
	if err := ListenAndServe(ctx, path, serverExport, serverImport); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListenAndServe(canceled) = %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket path created for canceled context: %v", err)
	}
	legacyExport, err := intercall.NewExportBinding(func(context.Context, uint64, []byte) (uint64, []byte) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := ListenAndServe(context.Background(), path, legacyExport, intercall.NewImportBinding()); !errors.Is(err, intercall.ErrInvalidArgument) {
		t.Fatalf("ListenAndServe(legacy) = %v", err)
	}
}

func TestListenAndServeReportsCleanupError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are not meaningful for root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "socket")
	serverExport, serverImport, _, _ := testBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- ListenAndServe(ctx, path, serverExport, serverImport)
	}()
	waitForPath(t, path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	cancel()
	err := <-result
	if !errors.Is(err, ErrServerClosed) {
		t.Fatalf("ListenAndServe() = %v, want ErrServerClosed", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("ListenAndServe() = %v, want cleanup permission error", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
