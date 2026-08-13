package intercall

// sentinel is a fixed, comparable error value with stable text and identity.
type sentinel string

// Error implements the error interface.
func (s sentinel) Error() string { return string(s) }

// Local error classifications. These sentinels work with direct comparison
// and errors.Is, including through wrapping with %w.
var (
	// ErrInvalidArgument reports an invalid argument passed to a runtime
	// constructor or method: a nil dispatch, context, receiver, stream
	// interface, encoder, or decoder, a zero binding passed to
	// NewConnection, or a zero procedure key.
	ErrInvalidArgument error = sentinel("intercall: invalid argument")

	// ErrNoConnection reports that no connection is bound in a context.
	ErrNoConnection error = sentinel("intercall: no connection")

	// ErrBindingMismatch reports a zero or different import handle passed to
	// Call on a valid connection.
	ErrBindingMismatch error = sentinel("intercall: binding mismatch")

	// ErrClosed reports an explicit close. Close selects ErrClosed if needed
	// and otherwise does nothing.
	ErrClosed error = sentinel("intercall: connection closed")

	// ErrRequestIDsExhausted reports that every outgoing request ID has been
	// allocated and the next call cannot proceed.
	ErrRequestIDsExhausted error = sentinel("intercall: request IDs exhausted")

	// ErrProtocol reports a terminal framing or matched-response protocol
	// error. Runtime conditions, not provider matching, select it.
	ErrProtocol error = sentinel("intercall: protocol error")

	// ErrInterfaceMismatch reports a negotiated interface ID that differs
	// from the local export interface ID. It is a setup error, not a frame
	// protocol error and not an authentication result.
	ErrInterfaceMismatch error = sentinel("intercall: interface mismatch")
)

// Fixed Go runtime wire exceptions. These are the three no-payload wire
// exceptions that export inserts into every interface and import maps to
// shared root sentinels. Each sentinel's Error string is its exact wire name.
// Runtime conditions, not provider matching, select them.
var (
	// ErrProcedureNotFound is the wire exception "procedure_not_found": a
	// fully framed unknown request.
	ErrProcedureNotFound error = sentinel("procedure_not_found")

	// ErrInvalidArguments is the wire exception "invalid_arguments": malformed
	// or trailing request arguments.
	ErrInvalidArguments error = sentinel("invalid_arguments")

	// ErrInternalException is the wire exception "internal_exception": a
	// provider, matching, or response-encoding failure.
	ErrInternalException error = sentinel("internal_exception")
)
