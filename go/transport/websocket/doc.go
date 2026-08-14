// Package websocket provides binary WebSocket transports for InterCall.
//
// DialStream and AcceptStream are low-level operations. They return a
// continuous byte stream whose boundaries are independent of WebSocket
// messages; text messages are rejected by the binary stream adapter. Dial,
// NewHandler, and ListenAndServe add the root package's directional interface
// negotiation. Interface IDs detect contract mismatches but do not
// authenticate peers. Contexts own accepted streams and connections, and
// adapter Close stops active WebSocket I/O without waiting for a graceful
// WebSocket close handshake.
package websocket
