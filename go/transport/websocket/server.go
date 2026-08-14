package websocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

const readHeaderTimeout = 10 * time.Second

func validateHTTPPath(path string) error {
	if path == "" || path[0] != '/' || strings.ContainsAny(path, "?#") {
		return fmt.Errorf("websocket: invalid literal HTTP path %q: %w", path, intercall.ErrInvalidArgument)
	}
	return nil
}

type exactPathHandler struct {
	path    string
	handler *Handler
}

func (h exactPathHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.URL == nil || r.URL.Path != h.path {
		http.NotFound(w, r)
		return
	}
	h.handler.ServeHTTP(w, r)
}

// ListenAndServe runs a plain HTTP WebSocket server at the exact decoded
// request path. TLS termination and authentication remain outside this
// convenience server.
func ListenAndServe(ctx context.Context, address, path string, export intercall.ExportBinding, imp intercall.ImportBinding) error {
	if ctx == nil {
		return fmt.Errorf("websocket: nil serving context: %w", intercall.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if address == "" {
		return fmt.Errorf("websocket: empty listen address: %w", intercall.ErrInvalidArgument)
	}
	if err := validateHTTPPath(path); err != nil {
		return err
	}
	if err := validateNegotiatedBindings(ctx, export, imp); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("websocket: listen %q: %w", address, err)
	}
	h := NewHandler(export, imp)
	server := &http.Server{
		Handler:           exactPathHandler{path: path, handler: h},
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return serve(ctx, listener, server, h)
}

func serve(ctx context.Context, listener net.Listener, server *http.Server, handler *Handler) error {
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	var primary error
	var shutdownErr error
	select {
	case primary = <-serveDone:
		if ctx.Err() != nil {
			primary = http.ErrServerClosed
		}
		_ = server.Close()
		shutdownErr = handler.Shutdown(context.Background())
	case <-ctx.Done():
		primary = http.ErrServerClosed
		shutdownDone := make(chan error, 1)
		go func() { shutdownDone <- handler.Shutdown(context.Background()) }()
		_ = server.Close()
		<-serveDone
		shutdownErr = <-shutdownDone
	}
	if primary == nil || errors.Is(primary, http.ErrServerClosed) {
		if ctx.Err() != nil {
			return http.ErrServerClosed
		}
		if shutdownErr != nil {
			return shutdownErr
		}
		return primary
	}
	return primary
}
