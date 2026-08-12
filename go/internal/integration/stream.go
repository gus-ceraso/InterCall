package integration

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/cerasos/intercall/go"
)

// bytePipe is one direction of a duplex stream: a mutex-guarded byte
// buffer with a condition variable. Read blocks until bytes are
// available or the pipe closes; Write appends and never blocks; and
// Close unblocks every waiter exactly once. Bytes are delivered
// reliably and in order. After close, a reader drains the buffered
// bytes first and then reports the pipe's drain error, so a partial
// frame followed by EOF is a truncated read, matching io.ReadFull
// semantics.
type bytePipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
	derr   error
}

func newBytePipe() *bytePipe {
	p := &bytePipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *bytePipe) write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.buf = append(p.buf, b...)
	p.cond.Broadcast()
	return len(b), nil
}

func (p *bytePipe) read(dst []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.buf) == 0 && !p.closed {
		p.cond.Wait()
	}
	if len(p.buf) == 0 {
		return 0, p.derr
	}
	n := copy(dst, p.buf)
	p.buf = p.buf[n:]
	return n, nil
}

func (p *bytePipe) closeWith(err error) {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		p.derr = err
		p.cond.Broadcast()
	}
	p.mu.Unlock()
}

// duplex is a buffered full-duplex ByteStream: two byte pipes, one per
// direction, so one read and one write may proceed concurrently, writes
// never block, and Close unblocks both. Closing one end closes both of
// its pipes with io.EOF: the local read side drains then reports EOF,
// terminating the connection's receive loop, and the remote side drains
// then reports EOF to the opposing end, so the peer observes the
// connection going away. A write to a closed pipe reports
// io.ErrClosedPipe. newDuplex returns the two ends of one stream;
// NewConnection takes ownership of one end and the test holds the
// other.
type duplex struct {
	local  *bytePipe // bytes the peer writes, this end reads
	remote *bytePipe // bytes this end writes, the peer reads
}

var _ intercall.ByteStream = (*duplex)(nil)

func newDuplex() (a, b *duplex) {
	ab := newBytePipe() // carries b -> a
	ba := newBytePipe() // carries a -> b
	return &duplex{local: ab, remote: ba}, &duplex{local: ba, remote: ab}
}

func (d *duplex) Read(p []byte) (int, error)  { return d.local.read(p) }
func (d *duplex) Write(p []byte) (int, error) { return d.remote.write(p) }
func (d *duplex) Close() error {
	d.local.closeWith(io.EOF)
	d.remote.closeWith(io.EOF)
	return nil
}

// failStream wraps one ByteStream with a switchable write failure for
// the shutdown write-race test: while armed, every Write returns the
// injected error without touching the underlying stream, so the
// connection's write gate reports a terminal transport error. Reads and
// Close delegate to the wrapped stream.
type failStream struct {
	intercall.ByteStream
	armed *atomic.Bool
	err   error
}

func (s *failStream) Write(p []byte) (int, error) {
	if s.armed.Load() {
		return 0, s.err
	}
	return s.ByteStream.Write(p)
}
