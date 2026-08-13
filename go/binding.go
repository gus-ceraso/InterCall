package intercall

import (
	"context"
	"io"
)

// ByteStream is the byte-stream boundary between the runtime and a transport.
//
// The stream must allow one read and one write concurrently, make Close
// unblock both, deliver bytes reliably and in order, and begin at the first
// InterCall frame. EOF and either half-close terminate the whole connection.
// Raw NewConnection does not dial, listen, negotiate, or assign initiator
// and acceptor roles; metadata-aware negotiated constructors perform only
// their explicit interface agreement.
type ByteStream interface {
	io.Reader
	io.Writer
	io.Closer
}

// InterfaceID identifies an interface for optional agreement between peers.
// It is metadata, not process-local binding identity or an authentication
// credential.
type InterfaceID [32]byte

// Dispatch is the generated export-side procedure dispatch function.
//
// A generated export package passes its static dispatch to NewExportBinding.
// The runtime invokes it with the handler context, the incoming procedure
// key, and the complete owned request payload, and it returns the exception
// key — zero for the procedure's success value — and the complete owned
// response payload.
type Dispatch func(
	context.Context,
	uint64, // procedure key
	[]byte, // complete owned request payload
) (uint64, []byte) // exception key and complete owned response payload

// RequestEncoder is the generated import-side request encoder closure.
//
// The runtime invokes it at most once per call, after local validation, and
// it must return one complete owned payload. A nil payload returned by a
// successful encoder is a valid empty payload.
type RequestEncoder func() ([]byte, error)

// ResponseDecoder is the generated import-side response decoder closure.
//
// The runtime invokes it with the exception key — zero for the procedure's
// success value — and the complete owned response payload. Returning nil
// means the decoder accepted one declared exception or success value,
// consumed the payload exactly, and stored the typed result in its closure.
type ResponseDecoder func(
	uint64, // exception key
	[]byte, // complete owned response payload
) error

// ExportBinding is an opaque handle to one generated export package's
// immutable binding state.
//
// The handle contains an unexported pointer to immutable, non-zero-sized
// runtime identity state, the package's dispatch function, and optional
// interface metadata. Copying the handle copies the pointer and retains
// process-local identity; the nil-pointer zero value is invalid. Independently
// constructed handles have distinct state addresses. Any number of
// connections may share a binding concurrently.
type ExportBinding struct {
	state *exportState
}

// exportState is the immutable identity state behind an ExportBinding.
// The identity byte is never read or written; its only purpose is to make the
// state non-zero-sized so that pointers to independently constructed states
// are always distinct.
type exportState struct {
	dispatch       Dispatch
	interfaceID    InterfaceID
	hasInterfaceID bool
	identity       byte
}

// ImportBinding is an opaque handle to one generated import package's
// immutable binding state.
//
// The handle contains an unexported pointer to immutable, non-zero-sized
// runtime identity state and optional interface metadata. Copying the handle
// copies the pointer and retains process-local identity; the nil-pointer zero
// value is invalid. Independently constructed handles have distinct state
// addresses. Any number of connections may share a binding concurrently.
type ImportBinding struct {
	state *importState
}

// importState is the immutable identity state behind an ImportBinding.
// The identity byte is never read or written; its only purpose is to make the
// state non-zero-sized so that pointers to independently constructed states
// are always distinct.
type importState struct {
	interfaceID    InterfaceID
	hasInterfaceID bool
	identity       byte
}

// NewExportBinding constructs the binding for one generated export package.
//
// It rejects a nil dispatch function with ErrInvalidArgument and otherwise
// allocates fresh, non-zero-sized identity state without interface metadata
// and returns a new handle.
func NewExportBinding(dispatch Dispatch) (ExportBinding, error) {
	if dispatch == nil {
		return ExportBinding{}, ErrInvalidArgument
	}
	return ExportBinding{state: &exportState{dispatch: dispatch}}, nil
}

// NewExportBindingWithInterfaceID constructs an export binding carrying id.
//
// It rejects a nil dispatch function with ErrInvalidArgument, records the ID
// even when it is all zero, and allocates fresh, non-zero-sized identity
// state. The ID is separate from the process-local handle identity.
func NewExportBindingWithInterfaceID(dispatch Dispatch, id InterfaceID) (ExportBinding, error) {
	if dispatch == nil {
		return ExportBinding{}, ErrInvalidArgument
	}
	return ExportBinding{state: &exportState{
		dispatch:       dispatch,
		interfaceID:    id,
		hasInterfaceID: true,
	}}, nil
}

// InterfaceID reports the optional interface metadata carried by b.
//
// A zero binding, or a binding made by NewExportBinding, reports ok == false.
// An ID supplied to NewExportBindingWithInterfaceID is present even when it
// is all zero.
func (b ExportBinding) InterfaceID() (InterfaceID, bool) {
	if b.state == nil {
		return InterfaceID{}, false
	}
	return b.state.interfaceID, b.state.hasInterfaceID
}

// NewImportBinding constructs the binding for one generated import package.
//
// It allocates fresh, non-zero-sized identity state without interface
// metadata and returns a new handle.
func NewImportBinding() ImportBinding {
	return ImportBinding{state: &importState{}}
}

// NewImportBindingWithInterfaceID constructs an import binding carrying id.
//
// It records the ID even when it is all zero and allocates fresh,
// non-zero-sized identity state. The ID is separate from the process-local
// handle identity.
func NewImportBindingWithInterfaceID(id InterfaceID) ImportBinding {
	return ImportBinding{state: &importState{
		interfaceID:    id,
		hasInterfaceID: true,
	}}
}

// InterfaceID reports the optional interface metadata carried by b.
//
// A zero binding, or a binding made by NewImportBinding, reports ok == false.
// An ID supplied to NewImportBindingWithInterfaceID is present even when it
// is all zero.
func (b ImportBinding) InterfaceID() (InterfaceID, bool) {
	if b.state == nil {
		return InterfaceID{}, false
	}
	return b.state.interfaceID, b.state.hasInterfaceID
}
