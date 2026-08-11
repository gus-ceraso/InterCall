package syntax

import (
	"strings"
	"testing"
)

func TestPositionLinesAndColumns(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		offset int
		want   Position
	}{
		{"empty eof", "", 0, Position{0, 1, 1}},
		{"ascii", "abc", 0, Position{0, 1, 1}},
		{"ascii end", "abc", 3, Position{3, 1, 4}},
		{"ascii eof", "abc", 3, Position{3, 1, 4}},
		{"one lf", "a\nb", 0, Position{0, 1, 1}},
		{"after lf", "a\nb", 2, Position{2, 2, 1}},
		{"end after lf", "a\nb", 3, Position{3, 2, 2}},
		{"trailing lf eof", "a\n", 2, Position{2, 2, 1}},
		{"crlf", "a\r\nb", 0, Position{0, 1, 1}},
		{"crlf cr is a byte", "a\r\nb", 1, Position{1, 1, 2}},
		{"crlf lf breaks line", "a\r\nb", 2, Position{2, 1, 3}},
		{"crlf after break", "a\r\nb", 3, Position{3, 2, 1}},
		{"crlf eof", "a\r\nb", 4, Position{4, 2, 2}},
		{"bare cr no line break", "a\rb", 2, Position{2, 1, 3}},
		{"bare cr eof", "a\rb", 3, Position{3, 1, 4}},
		{"crlf crlf", "\r\n\r\n", 2, Position{2, 2, 1}},
		{"tab is one byte column", "a\tb", 2, Position{2, 1, 3}},
		{"multi line", "x\ny\nz", 4, Position{4, 3, 1}},
		{"multibyte utf8 counts bytes", "é\nx", 2, Position{2, 1, 3}},
		{"multibyte utf8 next line", "é\nx", 3, Position{3, 2, 1}},
		{"line starts", "ab\ncd\nef", 3, Position{3, 2, 1}},
		{"line starts mid", "ab\ncd\nef", 5, Position{5, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFile("f", []byte(tt.src))
			got := f.Position(tt.offset)
			if got != tt.want {
				t.Errorf("Position(%d) = %+v, want %+v", tt.offset, got, tt.want)
			}
		})
	}
}

func TestPositionEOFIsLen(t *testing.T) {
	// The EOF position must be exactly offset len(src) on the final line.
	tests := []struct {
		src     string
		wantEOF Position
	}{
		{"", Position{0, 1, 1}},
		{"type x uint8;", Position{13, 1, 14}},
		{"a\nb\n", Position{4, 3, 1}},
		{"a\r\nb", Position{4, 2, 2}},
	}
	for _, tt := range tests {
		f := NewFile("f", []byte(tt.src))
		if got := f.Position(len(tt.src)); got != tt.wantEOF {
			t.Errorf("src %q: EOF position = %+v, want %+v", tt.src, got, tt.wantEOF)
		}
	}
}

func TestPositionOutOfRangePanics(t *testing.T) {
	f := NewFile("f", []byte("abc"))
	for _, off := range []int{-1, 4, 100} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Position(%d) did not panic", off)
				}
			}()
			f.Position(off)
		}()
	}
}

func TestTextSpans(t *testing.T) {
	src := "type x uint8;"
	f := NewFile("f", []byte(src))
	tests := []struct {
		span Span
		want string
	}{
		{Span{0, 0}, ""},
		{Span{0, 4}, "type"},
		{Span{5, 6}, "x"},
		{Span{0, len(src)}, src},
	}
	for _, tt := range tests {
		if got := f.Text(tt.span); got != tt.want {
			t.Errorf("Text(%v) = %q, want %q", tt.span, got, tt.want)
		}
	}
}

func TestTextOutOfRangePanics(t *testing.T) {
	f := NewFile("f", []byte("abc"))
	for _, span := range []Span{{-1, 1}, {2, 1}, {0, 4}, {3, 5}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Text(%v) did not panic", span)
				}
			}()
			f.Text(span)
		}()
	}
}

func TestErrorString(t *testing.T) {
	e := &Error{Filename: "iface.intercall", Pos: Position{7, 1, 8}, Span: Span{7, 11}, Msg: "expected identifier, found 'list'"}
	if got, want := e.Error(), "iface.intercall:1:8: expected identifier, found 'list'"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	e2 := &Error{Pos: Position{0, 1, 1}, Msg: "boom"}
	if got, want := e2.Error(), "1:1: boom"; got != want {
		t.Errorf("Error() without filename = %q, want %q", got, want)
	}
}

func TestSpanString(t *testing.T) {
	if got, want := (Span{3, 9}).String(), "[3,9)"; got != want {
		t.Errorf("Span.String() = %q, want %q", got, want)
	}
}

func TestPositionString(t *testing.T) {
	if got, want := (Position{7, 1, 8}).String(), "1:8"; got != want {
		t.Errorf("Position.String() = %q, want %q", got, want)
	}
}

func TestNewFileNilSource(t *testing.T) {
	f := NewFile("f", nil)
	if f.Size != 0 {
		t.Errorf("Size = %d, want 0", f.Size)
	}
	if pos := f.Position(0); pos != (Position{0, 1, 1}) {
		t.Errorf("Position(0) = %+v, want {0 1 1}", pos)
	}
	if strings.Contains(f.Text(Span{0, 0}), "x") {
		t.Error("Text of nil source is not empty")
	}
}
