package intercall_test

import (
	"context"
	"fmt"
	"io"
	"sync"

	intercall "github.com/cerasos/intercall"
)

// The examples demonstrate the runtime's connection lifecycle over an
// in-memory duplex stream. Any transport that delivers complete InterCall
// frames reliably and in order implements the same ByteStream contract.

// examplePipe is one direction of an in-memory ByteStream: Write appends
// to a buffer, Read blocks until bytes arrive or the pipe closes, and
// Close unblocks every reader and writer. Bytes are delivered reliably
// and in order.
type examplePipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newExamplePipe() *examplePipe {
	p := &examplePipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *examplePipe) Read(dst []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.buf) == 0 && !p.closed {
		p.cond.Wait()
	}
	if len(p.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(dst, p.buf)
	p.buf = p.buf[n:]
	return n, nil
}

func (p *examplePipe) Write(src []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.buf = append(p.buf, src...)
	p.cond.Broadcast()
	return len(src), nil
}

func (p *examplePipe) Close() error {
	p.mu.Lock()
	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()
	return nil
}

// exampleDuplex is one end of a full-duplex ByteStream: local is the read
// side and remote is the write side, so one read and one write may proceed
// concurrently. Close closes both pipes, unblocking every reader on this
// end and delivering EOF to the opposing end, which observes the
// connection going away.
type exampleDuplex struct {
	local  *examplePipe // this end reads from
	remote *examplePipe // this end writes to
}

func newExampleDuplex() (a, b *exampleDuplex) {
	ab, ba := newExamplePipe(), newExamplePipe()
	return &exampleDuplex{local: ab, remote: ba}, &exampleDuplex{local: ba, remote: ab}
}

func (d *exampleDuplex) Read(p []byte) (int, error)  { return d.local.Read(p) }
func (d *exampleDuplex) Write(p []byte) (int, error) { return d.remote.Write(p) }
func (d *exampleDuplex) Close() error {
	_ = d.local.Close()
	return d.remote.Close()
}

// ExampleNewConnection walks through the complete runtime lifecycle over
// one in-memory duplex stream: construct both ends of a connection with
// the binding handles, bind one end into a context, place one outgoing
// call through the generated-code SPI, close both ends, and wait for the
// permanent terminal causes.
func ExampleNewConnection() {
	// One duplex stream joins the two peers. The runtime does not dial
	// or listen: the application supplies the established stream.
	serverSide, clientSide := newExampleDuplex()

	// The export side serves one procedure. Generated export bindings
	// provide exactly this Dispatch statically; this hand-written one
	// echoes the "get_user" payload and answers every other procedure
	// key with the fixed procedure_not_found exception.
	export, err := intercall.NewExportBinding(func(ctx context.Context, key uint64, payload []byte) (uint64, []byte) {
		if key == 0x4c63cc5048869eb7 { // procedure get_user
			return 0, payload // exception key 0 selects the return value
		}
		return 0x970e76fcc5e2dacb, nil // exception procedure_not_found
	})
	if err != nil {
		panic(err)
	}
	imp := intercall.NewImportBinding()

	// NewConnection validates every argument before taking ownership of
	// the stream and starts its receive loop before returning.
	server, err := intercall.NewConnection(context.Background(), serverSide, export, imp)
	if err != nil {
		panic(err)
	}
	client, err := intercall.NewConnection(context.Background(), clientSide, export, imp)
	if err != nil {
		panic(err)
	}

	// Generated import callers retrieve the connection from the context
	// with ConnectionFromContext; WithConnection binds it.
	ctx := intercall.WithConnection(context.Background(), client)

	// One outgoing call. Generated code passes one request-encoder and
	// one response-decoder closure; Call invokes the encoder at most
	// once and the decoder with the matched response.
	err = client.Call(ctx, imp, 0x4c63cc5048869eb7, // procedure get_user
		func() ([]byte, error) { return []byte("alice"), nil },
		func(key uint64, payload []byte) error {
			if key != 0 {
				return fmt.Errorf("unexpected exception key %d", key)
			}
			fmt.Printf("get_user returned %q\n", payload)
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// Close terminates the connection without waiting; Wait returns the
	// permanent terminal cause after teardown completes. The closed end
	// reports ErrClosed; the peer observes the connection going away as
	// EOF on its stream and terminates with that cause.
	_ = client.Close()
	fmt.Printf("client wait: %v\n", client.Wait())
	fmt.Printf("server wait: %v\n", server.Wait())

	// Output:
	// get_user returned "alice"
	// client wait: intercall: connection closed
	// server wait: intercall: read frame header: EOF
}

// ExampleConnectionFromContext shows the ErrNoConnection sentinel that
// generated import callers surface when the context carries no connection.
func ExampleConnectionFromContext() {
	_, err := intercall.ConnectionFromContext(context.Background())
	fmt.Println(err)

	// Output:
	// intercall: no connection
}
