package nav

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// hashLen is the number of hex digits in a block hash (48 bits).
const hashLen = 12

// BlockHash identifies the content of lines [from, to] of d: the first 12 hex
// digits of the SHA-256 of those lines joined by "\n". It depends only on the
// block's text, not on its position or the file's line terminators, so an
// edit elsewhere in the file or a CRLF/LF change leaves it unchanged. Edit
// commands compare it with --expect to refuse writing over a changed block.
func BlockHash(d *markdown.Doc, from, to int) string {
	h := sha256.New()
	for l := from; l <= to; l++ {
		if l > from {
			h.Write([]byte{'\n'})
		}
		h.Write([]byte(d.Line(l)))
	}
	return hex.EncodeToString(h.Sum(nil))[:hashLen]
}

// blockSpan returns the range an edit addressing line ln would cover: the
// smallest enclosing block, or the whole section when ln is a heading.
func blockSpan(d *markdown.Doc, ln int) (from, to int) {
	b, ok := d.Smallest(ln)
	switch {
	case !ok:
		return ln, ln
	case b.Kind == markdown.KindHeading:
		h := d.Headings[b.Heading]
		return h.Line, h.End
	}
	return b.Start, b.End
}

// spanHash is BlockHash of blockSpan(d, ln).
func spanHash(d *markdown.Doc, ln int) string {
	from, to := blockSpan(d, ln)
	return BlockHash(d, from, to)
}
