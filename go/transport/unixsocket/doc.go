//go:build unix

// Package unixsocket provides filesystem-backed Unix stream sockets for
// InterCall connections.
//
// DialStream and ListenStream are low-level transport operations. They do not
// perform interface negotiation and transfer ownership of successful streams
// to the caller. Dial and ListenAndServe, added by the higher-level API, use
// the root package's negotiated constructors. Unix socket paths are ordinary
// filesystem paths; abstract Linux sockets, embedded NUL bytes, and empty
// paths are rejected.
package unixsocket
