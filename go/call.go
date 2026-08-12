package intercall

import (
	"context"
	"encoding/binary"
	"fmt"
)

// pendingCall is one admitted outgoing request. Presence in the pending map
// means the admitted request is eligible for exactly one outcome; removal
// transfers exclusive ownership to exactly one of a matched response,
// per-call cancellation, or terminal teardown. There are no
// registered/writing/waiting/claimed enum states or tombstones.
type pendingCall struct {
	// decoder is the generated response decoder for this call, invoked by
	// the response claim hook with the matched exception key and the
	// complete owned response payload.
	decoder ResponseDecoder

	// done closes exactly once, when the outcome owner completes the entry;
	// result is then valid. Completion happens outside the connection lock,
	// and the waiting Call goroutine observes the result through the channel
	// close, so a response decoder's closure writes are visible before Call
	// returns.
	done   chan struct{}
	result error
}

// complete settles this pending call's outcome. The caller must hold
// exclusive ownership, proven by having removed the entry from the pending
// map, and must call complete exactly once per entry.
func (pc *pendingCall) complete(result error) {
	pc.result = result
	close(pc.done)
}

// Call places one outgoing InterCall request on the connection and waits for
// its single outcome: the matched response, per-call cancellation, or
// connection termination.
//
// Call is generated-code SPI: the generated import caller passes its exact
// import handle, one procedure key, and one request-encoder and one
// response-decoder closure. Call follows the SPEC.md ordering contract
// exactly:
//
//  1. it validates its receiver, context, exact import identity, procedure
//     key, encoder, and decoder, returning ErrInvalidArgument or
//     ErrBindingMismatch before any terminal-state inspection;
//  2. it returns an already selected terminal cause or an already available
//     ctx.Err(), with the terminal cause winning, without invoking the
//     encoder;
//  3. it invokes the encoder exactly once to obtain one complete owned
//     payload;
//  4. it returns the encoder's exact error, if any, without allocating an
//     ID, constructing a frame, or entering the write gate;
//  5. it rechecks terminal state and ctx.Err(), builds the owned contiguous
//     frame, and waits on the write gate while allowing terminal selection
//     or the call context's Done to win;
//  6. it rechecks both under the connection lock, allocates an ID, and
//     inserts one pending entry immediately before write admission;
//  7. it writes the whole buffered frame while holding the gate; and
//  8. after write completion, it waits for the response, per-call
//     cancellation, or connection termination.
//
// Outgoing IDs increase monotonically from 0 through 0x7fffffffffffffff and
// are never reused, including after completion or local cancellation. After
// allocating the final ID, the next call returns ErrRequestIDsExhausted
// without writing a frame. No ID is allocated while waiting for the write
// gate, the connection state lock is never held while waiting for the gate
// or while calling stream Write, and insertion and write admission are one
// lock-protected action. After admission the per-call context cannot
// interrupt the write; a response or connection termination may claim the
// entry during the full-duplex write, and that claim decides the outcome.
//
// A per-call context cancellation returns that context's exact
// context.Canceled or context.DeadlineExceeded when cancellation claims the
// call, without terminating the connection and without a cancellation frame;
// the retired ID makes any later response unmatched and opaque.
func (c *Connection) Call(ctx context.Context, imp ImportBinding, procedureKey uint64, encode RequestEncoder, decode ResponseDecoder) error {
	// Step 1: argument and binding validation, before any terminal-state
	// inspection. Generated paths always pass valid arguments.
	if c == nil {
		return ErrInvalidArgument
	}
	if ctx == nil {
		return ErrInvalidArgument
	}
	if procedureKey == 0 {
		return ErrInvalidArgument
	}
	if encode == nil {
		return ErrInvalidArgument
	}
	if decode == nil {
		return ErrInvalidArgument
	}
	if err := c.checkImport(imp); err != nil {
		return err
	}

	// Step 2: an already selected terminal cause or an already available
	// ctx.Err() returns without invoking the encoder.
	if err := c.callReady(ctx); err != nil {
		return err
	}

	// Step 3: invoke the encoder exactly once for one complete owned
	// payload. A nil payload is a valid empty payload.
	payload, err := encode()
	if err != nil {
		// Step 4: the encoder's exact error, with no ID, frame, or gate.
		return err
	}

	// Step 5: recheck terminal state and ctx.Err(), then build the owned
	// contiguous frame. The request ID field is a placeholder: IDs are
	// allocated only at write admission in step 6, never while waiting for
	// the gate, and the owned frame's ID bytes are patched there before the
	// write begins.
	if err := c.callReady(ctx); err != nil {
		return err
	}
	frame := buildFrame(requestFrame, 0, procedureKey, payload)

	// Step 6: wait for the write gate without holding the connection state
	// lock, allowing terminal selection and the call context's Done to win
	// the wait. No ID is allocated while waiting for the gate. Once the
	// gate is held, admission is one lock-protected action under the
	// connection lock: a cancellation or termination that fired while
	// waiting wins without an ID or frame, ID exhaustion is checked, then
	// the next monotonic ID is allocated and one pending entry is inserted
	// immediately before the write.
	if err := c.acquireWriteGate(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	if err := c.callReadyLocked(ctx); err != nil {
		c.mu.Unlock()
		c.releaseWriteGate()
		return err
	}
	if c.nextID > idMask {
		c.mu.Unlock()
		c.releaseWriteGate()
		return ErrRequestIDsExhausted
	}
	id := c.nextID
	c.nextID++
	pc := &pendingCall{decoder: decode, done: make(chan struct{})}
	c.pending[id] = pc
	binary.LittleEndian.PutUint64(frame[0:8], id)
	c.mu.Unlock()

	// Step 7: write the whole buffered frame while holding the gate. After
	// admission the per-call context cannot interrupt the write, close the
	// stream, or claim the pending entry; a response or connection
	// termination may claim the entry during the full-duplex write. A write
	// failure terminates the connection: if a response already removed the
	// entry, that response remains this call's outcome; otherwise terminal
	// teardown claims it.
	werr := writeFull(c.stream, frame)
	c.releaseWriteGate()
	if werr != nil {
		c.selectTerminal(werr)
	}

	// Step 8: wait for the single outcome. The pending map decides: the
	// first claim — response, per-call cancellation, or terminal teardown —
	// that removes the entry owns the outcome exclusively.
	select {
	case <-pc.done:
		return pc.result
	case <-ctx.Done():
		if c.claimCancel(id, pc, ctx.Err()) {
			return ctx.Err()
		}
		<-pc.done
		return pc.result
	}
}

// callReady reports whether an outgoing call may proceed: it returns the
// already selected terminal cause or an already available ctx.Err() under
// the connection lock, and nil when both are clear. An already selected
// terminal cause wins over an already canceled call context; otherwise the
// exact context error wins.
func (c *Connection) callReady(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callReadyLocked(ctx)
}

// callReadyLocked is callReady with the connection lock already held.
func (c *Connection) callReadyLocked(ctx context.Context) error {
	if c.cause != nil {
		return c.cause
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// claimResponse is the response claim hook that the receive loop calls with
// each completely buffered response frame: the request ID, the exception
// key, and the complete owned payload. It returns true when the response
// matched a pending entry and false when the ID is unmatched; an unmatched
// response stays opaque and its payload is not validated.
//
// A matched response claims the call by removing its entry under the
// connection lock, then invokes the call's generated decoder in the calling
// (receive) goroutine. A nil decoder result accepts one declared exception
// or success value and completes the entry successfully. A decoder error or
// panic terminates the connection and completes the entry with the
// permanent terminal cause, which wraps ErrProtocol for a matched-response
// error. The removed entry is completed exactly once, so the claim is
// exclusive against per-call cancellation and terminal teardown.
func (c *Connection) claimResponse(id uint64, key uint64, payload []byte) bool {
	c.mu.Lock()
	pc, ok := c.pending[id]
	if !ok {
		c.mu.Unlock()
		return false
	}
	delete(c.pending, id)
	c.mu.Unlock()

	if err := pc.invokeDecoder(key, payload); err != nil {
		c.selectTerminal(fmt.Errorf("intercall: matched response %d: %v: %w", id, err, ErrProtocol))
		// Complete with the permanent terminal cause: this decoder failure
		// selected it, or a concurrent terminal event already did.
		c.mu.Lock()
		cause := c.cause
		c.mu.Unlock()
		pc.complete(cause)
		return true
	}
	pc.complete(nil)
	return true
}

// claimCancel is the per-call cancellation claim. The waiting Call goroutine
// invokes it when its context fires: removal from the pending map transfers
// exclusive ownership to cancellation, which retires the ID permanently and
// completes the entry with the exact context error. A false return means
// another outcome — a response or terminal teardown — already claimed the
// entry, and the caller waits for that outcome's completion instead.
func (c *Connection) claimCancel(id uint64, pc *pendingCall, cause error) bool {
	c.mu.Lock()
	if _, ok := c.pending[id]; !ok {
		c.mu.Unlock()
		return false
	}
	delete(c.pending, id)
	c.mu.Unlock()
	pc.complete(cause)
	return true
}

// invokeDecoder runs the pending call's response decoder and converts any
// panic into an error: a decoder error or panic terminates the connection
// and completes the entry with the permanent terminal cause.
func (pc *pendingCall) invokeDecoder(key uint64, payload []byte) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("intercall: response decoder panic: %v", v)
		}
	}()
	return pc.decoder(key, payload)
}
