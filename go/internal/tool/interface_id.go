package tool

// emitInterfaceIDLiteral emits one deterministic intercall.InterfaceID
// composite literal. Bytes are rendered as fixed-width lowercase hexadecimal
// values in digest order, one per line, so generated source is stable and
// easy to inspect without depending on the artifact stamp text.
func emitInterfaceIDLiteral(src *source, sum [32]byte) {
	src.linef("intercall.InterfaceID{")
	src.open()
	for _, b := range sum {
		src.linef("0x%02x,", b)
	}
	src.close()
	src.linef("},")
}
