//go:build unix

package unixsocket

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"

	intercall "github.com/cerasos/intercall/go"
)

// ListenOptions controls a Unix socket listener. Mode is applied after a
// successful bind; zero selects 0600.
type ListenOptions struct {
	Mode fs.FileMode
}

// Listener owns a Unix stream listener and, when it created the socket path,
// removes that path only while it still identifies the created socket.
type Listener struct {
	mu       sync.Mutex
	listener *net.UnixListener
	path     string
	identity os.FileInfo

	closeOnce sync.Once
	closeErr  error
}

var _ net.Listener = (*Listener)(nil)

// anchoredPath captures the filesystem path used by a listener before any
// setup operation. Relative paths are anchored without lexical cleaning or
// symlink resolution, so a later chdir cannot retarget the listener.
func anchoredPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("unixsocket: get working directory: %w", err)
	}
	if cwd == string(filepath.Separator) {
		return cwd + path, nil
	}
	return cwd + string(filepath.Separator) + path, nil
}

// ListenStream creates an owning Unix stream listener at path. Every
// pre-existing filesystem entry at the leaf is rejected; stale sockets are
// never removed automatically.
func ListenStream(path string, options *ListenOptions) (*Listener, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	mode := fs.FileMode(0600)
	if options != nil {
		mode = options.Mode
		if mode == 0 {
			mode = 0600
		}
	}
	if mode.Perm() != mode {
		return nil, fmt.Errorf("unixsocket: unsupported socket mode %04o: %w", mode, intercall.ErrInvalidArgument)
	}
	stablePath, err := anchoredPath(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(stablePath); err == nil {
		return nil, fmt.Errorf("unixsocket: socket path already exists: %q", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("unixsocket: inspect socket path %q: %w", path, err)
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: stablePath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("unixsocket: listen %q: %w", path, err)
	}
	listener.SetUnlinkOnClose(false)

	info, err := os.Lstat(stablePath)
	if err != nil || info.Mode()&fs.ModeSocket == 0 {
		_ = listener.Close()
		if err != nil {
			return nil, fmt.Errorf("unixsocket: identify created socket %q: %w", path, err)
		}
		return nil, fmt.Errorf("unixsocket: bound path %q is not a socket", path)
	}
	l := &Listener{listener: listener, path: stablePath, identity: info}
	if err := os.Chmod(stablePath, mode.Perm()); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("unixsocket: set socket mode %q: %w", path, err)
	}
	return l, nil
}

// AcceptStream accepts one Unix stream connection.
func (l *Listener) AcceptStream() (*net.UnixConn, error) {
	if l == nil {
		return nil, fmt.Errorf("unixsocket: nil listener: %w", intercall.ErrInvalidArgument)
	}
	l.mu.Lock()
	listener := l.listener
	l.mu.Unlock()
	if listener == nil {
		return nil, fmt.Errorf("unixsocket: listener is unavailable: %w", net.ErrClosed)
	}
	return listener.AcceptUnix()
}

// Accept implements net.Listener.
func (l *Listener) Accept() (net.Conn, error) {
	return l.AcceptStream()
}

// Close closes the listener and removes its owned socket path if the path
// still identifies the socket created by ListenStream. It is idempotent.
func (l *Listener) Close() error {
	if l == nil {
		return fmt.Errorf("unixsocket: nil listener: %w", intercall.ErrInvalidArgument)
	}
	l.closeOnce.Do(func() {
		l.mu.Lock()
		listener, path, identity := l.listener, l.path, l.identity
		l.mu.Unlock()
		var first error
		if listener != nil {
			first = listener.Close()
		}
		if current, err := os.Lstat(path); err == nil {
			if os.SameFile(current, identity) {
				if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) && first == nil {
					first = fmt.Errorf("unixsocket: remove socket path %q: %w", path, err)
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) && first == nil {
			first = fmt.Errorf("unixsocket: inspect socket path %q during close: %w", path, err)
		}
		l.mu.Lock()
		l.closeErr = first
		l.mu.Unlock()
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closeErr
}

// Addr returns the listener's Unix address. A nil receiver returns nil to
// match net.Listener's no-error address method.
func (l *Listener) Addr() net.Addr {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	listener := l.listener
	l.mu.Unlock()
	if listener == nil {
		return nil
	}
	return listener.Addr()
}
