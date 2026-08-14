//go:build unix

package unixsocket

import (
	"context"
	"errors"
	"fmt"
	"sync"

	intercall "github.com/cerasos/intercall/go"
)

// ErrServerClosed is returned when ListenAndServe stops because its serving
// context was canceled after serving began.
var ErrServerClosed = errors.New("unixsocket: server closed")

type serverState struct {
	mu       sync.Mutex
	stopping bool
	setup    map[*closeOnceStream]struct{}
	active   map[*intercall.Connection]struct{}
	wg       sync.WaitGroup
}

func (s *serverState) addSetup(stream *closeOnceStream) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false
	}
	s.setup[stream] = struct{}{}
	s.wg.Add(1)
	return true
}

func (s *serverState) promote(stream *closeOnceStream, conn *intercall.Connection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.setup, stream)
	if s.stopping {
		return false
	}
	s.active[conn] = struct{}{}
	return true
}

func (s *serverState) remove(stream *closeOnceStream, conn *intercall.Connection) {
	s.mu.Lock()
	delete(s.setup, stream)
	if conn != nil {
		delete(s.active, conn)
	}
	s.mu.Unlock()
}

func (s *serverState) stop() (setup []*closeOnceStream, active []*intercall.Connection) {
	s.mu.Lock()
	if !s.stopping {
		s.stopping = true
	}
	for stream := range s.setup {
		setup = append(setup, stream)
	}
	for conn := range s.active {
		active = append(active, conn)
	}
	s.mu.Unlock()
	return setup, active
}

// ListenAndServe creates a default-permission Unix listener and serves
// negotiated InterCall connections until ctx is canceled or the listener
// fails. It returns ErrServerClosed for context-driven shutdown.
func ListenAndServe(ctx context.Context, path string, export intercall.ExportBinding, imp intercall.ImportBinding) error {
	if ctx == nil {
		return fmt.Errorf("unixsocket: nil serving context: %w", intercall.ErrInvalidArgument)
	}
	if err := validatePath(path); err != nil {
		return err
	}
	if err := validateConnectionArguments(ctx, export, imp); err != nil {
		return err
	}
	listener, err := ListenStream(path, nil)
	if err != nil {
		return err
	}
	return serve(ctx, listener, export, imp)
}

func serve(ctx context.Context, listener *Listener, export intercall.ExportBinding, imp intercall.ImportBinding) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := &serverState{
		setup:  make(map[*closeOnceStream]struct{}),
		active: make(map[*intercall.Connection]struct{}),
	}
	stopWatcher := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stopWatcher:
		}
	}()

	var primary error
	for {
		stream, err := listener.AcceptStream()
		if err != nil {
			if ctx.Err() != nil {
				primary = ErrServerClosed
			} else {
				primary = fmt.Errorf("unixsocket: accept: %w", err)
			}
			break
		}
		owner := &closeOnceStream{stream: stream}
		if !state.addSetup(owner) {
			_ = owner.Close()
			continue
		}
		go func() {
			defer state.wg.Done()
			conn, err := intercall.NewNegotiatedServerConnection(serveCtx, owner, export, imp)
			if err != nil {
				state.remove(owner, nil)
				_ = owner.Close()
				return
			}
			if !state.promote(owner, conn) {
				_ = conn.Close()
				_ = conn.Wait()
				return
			}
			_ = conn.Wait()
			state.remove(owner, conn)
		}()
	}

	setup, active := state.stop()
	cancel()
	_ = listener.Close()
	close(stopWatcher)
	<-watcherDone
	for _, stream := range setup {
		_ = stream.Close()
	}
	for _, conn := range active {
		_ = conn.Close()
	}
	state.wg.Wait()
	if primary == ErrServerClosed {
		return ErrServerClosed
	}
	return primary
}
