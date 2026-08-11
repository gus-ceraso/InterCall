package intercall

import (
	"context"
	"fmt"
)

// internalExceptionKey is the FNV-0 key of the fixed no-payload wire
// exception "internal_exception" (SPEC.md Fixed Go Runtime Exceptions). The
// runtime's one recovery around the complete dispatch selects it when a
// dispatch or provider panic escapes. The generated static procedure switch
// selects procedure_not_found and invalid_arguments itself.
const internalExceptionKey = 0x1aaec22e85996f50

// receiveLoop is the sole reader of the connection's stream, started by
// NewConnection before it returns. It reads one complete frame at a time
// with full-read semantics: each 24-byte header, then the complete payload
// in a fresh owned buffer after the wire length has been checked against
// the native int size. A response is completely buffered before lookup: a
// pending ID is claimed and decoded in this goroutine, while an unmatched ID
// stays opaque and its payload is never validated. Each request transfers
// its complete payload to one new, unbounded handler goroutine after its
// incoming request ID is reserved; reuse of an active ID is a terminal
// protocol error. Every read failure is terminal: an incomplete header or
// payload is a transport failure, and an impossible native size or
// structural frame failure is a protocol error; readFrame classifies and
// prefixes both.
func (c *Connection) receiveLoop() {
	defer close(c.receiveExit)
	for {
		hdr, payload, err := readFrame(c.stream)
		if err != nil {
			c.selectTerminal(err)
			return
		}
		switch hdr.kind {
		case responseFrame:
			c.claimResponse(hdr.requestID, hdr.key, payload)
		case requestFrame:
			// Reserve the incoming request ID before starting the handler:
			// reuse before the earlier response write completes is a
			// terminal protocol error.
			if !c.reserveIncoming(hdr.requestID) {
				c.selectTerminal(fmt.Errorf("intercall: incoming request ID %d already active: %w", hdr.requestID, ErrProtocol))
				return
			}
			go c.handleRequest(hdr.requestID, hdr.key, payload)
		}
	}
}

// reserveIncoming reserves an incoming request ID in the active set before
// the receive loop starts a handler goroutine. A false return reports that
// the ID is already active — its earlier response write has not completed —
// making the reuse a terminal protocol error. Incoming and outgoing ID
// spaces are independent, and reuse after release is allowed.
func (c *Connection) reserveIncoming(id uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.incoming[id]; ok {
		return false
	}
	c.incoming[id] = struct{}{}
	return true
}

// releaseIncoming removes an incoming request ID from the active set after
// its complete response write succeeds.
func (c *Connection) releaseIncoming(id uint64) {
	c.mu.Lock()
	delete(c.incoming, id)
	c.mu.Unlock()
}

// handleRequest executes one incoming call in its own unbounded handler
// goroutine. The receive loop reserved the request ID before starting it and
// transferred the complete owned payload, which the runtime never reuses.
// The handler derives one bound per-handler context, invokes the export
// dispatch under the single runtime recovery that maps escaped panics to
// internal_exception, fully encodes the selected response, and writes it
// through the shared write gate. The incoming ID remains active until the
// complete response write succeeds, and the handler context is canceled when
// the handler finishes or the connection terminates. Terminal state prevents
// a handler from beginning a later response write: a handler that observes
// terminal selection at write admission abandons its response, and a handler
// already writing is unblocked by stream closure.
func (c *Connection) handleRequest(id, key uint64, payload []byte) {
	hctx, hcancel := c.newHandlerContext()
	defer hcancel()

	ekey, epayload := c.invokeDispatch(hctx, key, payload)
	frame := buildFrame(responseFrame, id, ekey, epayload)

	// Write admission: acquire the gate, then recheck terminal state under
	// the connection lock so a handler waiting for the gate abandons its
	// response after terminal selection. The lock order is mu then writeMu.
	c.mu.Lock()
	c.writeMu.Lock()
	if c.cause != nil {
		c.writeMu.Unlock()
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	err := writeFull(c.stream, frame)
	c.writeMu.Unlock()
	if err != nil {
		c.selectTerminal(err)
		return
	}
	c.releaseIncoming(id)
}

// invokeDispatch runs the connection's export dispatch with the handler
// context, the incoming procedure key, and the complete owned request
// payload. One recovery around the complete dispatch maps every escaped
// panic to the internal_exception response; the generated static procedure
// switch selects procedure_not_found and invalid_arguments itself before
// invoking a provider.
func (c *Connection) invokeDispatch(ctx context.Context, key uint64, payload []byte) (ekey uint64, epayload []byte) {
	defer func() {
		if recover() != nil {
			ekey = internalExceptionKey
			epayload = nil
		}
	}()
	return c.export.state.dispatch(ctx, key, payload)
}
