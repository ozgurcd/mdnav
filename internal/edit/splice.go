package edit

import (
	"bytes"
	"strings"
)

// text is a file's bytes with the byte offset of each line.
type text struct {
	raw     []byte
	starts  []int  // starts[i] is the offset of line i+1; starts[n] is len(raw)
	eol     string // "\r\n" when the first line ends that way, else "\n"
	finalNL bool   // the last line ends with a terminator (true for an empty file)
}

func newText(raw []byte) text {
	t := text{raw: raw, eol: "\n", finalNL: true}
	if i := bytes.IndexByte(raw, '\n'); i > 0 && raw[i-1] == '\r' {
		t.eol = "\r\n"
	}
	for off := 0; off < len(raw); {
		t.starts = append(t.starts, off)
		i := bytes.IndexByte(raw[off:], '\n')
		if i < 0 {
			t.finalNL = false
			break
		}
		off += i + 1
	}
	t.starts = append(t.starts, len(raw))
	return t
}

func (t text) numLines() int { return len(t.starts) - 1 }

// splice replaces lines from..to (1-based, inclusive) with lines. to ==
// from-1 inserts before line from; from == numLines()+1 appends. New lines
// use the file's terminator. Bytes outside the range are kept exactly, and
// the file's trailing-newline state is preserved.
func (t text) splice(from, to int, lines []string) []byte {
	n := t.numLines()
	begin, end := t.starts[from-1], t.starts[to]
	body := strings.Join(lines, t.eol)
	atEnd := end == len(t.raw)
	switch {
	case len(lines) == 0:
		body = ""
		// Removing the last line of a file without a final newline: drop the
		// terminator before it too, so no empty line is left behind.
		if atEnd && !t.finalNL && begin > 0 {
			begin -= len(t.eolBefore(begin))
		}
	case to == from-1 && from == n+1 && !t.finalNL && n > 0:
		body = t.eol + body // append after an unterminated last line
	case atEnd && !t.finalNL:
		// The range ends the file, which has no final newline: keep it so.
	default:
		body += t.eol
	}
	out := make([]byte, 0, len(t.raw)-(end-begin)+len(body))
	out = append(out, t.raw[:begin]...)
	out = append(out, body...)
	return append(out, t.raw[end:]...)
}

// eolBefore returns the line terminator that ends just before offset off.
func (t text) eolBefore(off int) string {
	if off >= 2 && t.raw[off-2] == '\r' && t.raw[off-1] == '\n' {
		return "\r\n"
	}
	return "\n"
}
