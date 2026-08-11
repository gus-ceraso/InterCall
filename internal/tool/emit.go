package tool

import (
	"bytes"
	"fmt"
)

// source is a small source-emission buffer with tab indentation
// tracking. The generated code is formatted with go/format after
// emission, so the buffer records line structure and indentation only;
// spacing inside a line still follows the emitted text.
//
// A source is not safe for concurrent use.
type source struct {
	b   bytes.Buffer
	ind int
}

// linef emits one indented line ending in a newline.
func (s *source) linef(format string, args ...any) {
	for i := 0; i < s.ind; i++ {
		s.b.WriteByte('\t')
	}
	fmt.Fprintf(&s.b, format, args...)
	s.b.WriteByte('\n')
}

// open increases the indentation of following lines.
func (s *source) open() { s.ind++ }

// close decreases the indentation of following lines.
func (s *source) close() { s.ind-- }

// blank emits one empty line.
func (s *source) blank() { s.b.WriteByte('\n') }

// bytes returns the emitted source bytes.
func (s *source) bytes() []byte { return s.b.Bytes() }
