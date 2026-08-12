// Package integration exercises the generated import and export
// bindings of the checked-in fixture modules end to end over established
// byte streams: the immutable binding handles, the generated callers and
// dispatch, the codecs, the exception mapping, and the complete
// connection lifecycle of the root runtime, joined as real black-box
// peers.
//
// The fixture modules under fixtures/ are the durable generated peer
// pairs: the handwritten provider package, the checked-in export binding
// and owned interface generated from it by intercall-go, and the
// checked-in import binding generated from that interface. The harness
// in this package supplies the buffered duplex byte stream, the raw
// frame builders and malformed-stream helpers, and the connection and
// synchronization helpers the tests share. The regeneration tests write
// only into temporary directories and byte-compare against the
// checked-in fixtures; validation never edits them.
package integration
