// Package nav answers navigation queries over parsed Markdown documents:
// get a block by identifier, section number, heading or text; list an
// identifier's references; hint at supersession; outline a file; list ids.
//
// Results are plain data. Ordering is deterministic: files in sorted path
// order, then line order.
package nav

import (
	"strings"
	"unicode/utf8"

	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
)

// Defaults for Options.
const (
	DefaultGetLimit  = 10
	DefaultListLimit = 200
	DefaultMaxLines  = 200
	snippetWidth     = 200
)

// Options tunes a query. Zero values mean "use the default" except where
// noted.
type Options struct {
	Limit      int  // get: blocks; refs, status, ids: entries. Negative means no limit.
	MaxLines   int  // cap on lines printed for one block or section
	Regex      bool // get: treat the query as a regular expression over text
	IncludeDef bool // refs: include the defining row
	Full       bool // refs, status: print whole lines, not snippets
	Depth      int  // outline: deepest heading level to show (0 = all)
	Pattern    string
}

func (o Options) maxLines() int {
	if o.MaxLines > 0 {
		return o.MaxLines
	}
	return DefaultMaxLines
}

// limit returns the effective limit, or -1 for none.
func (o Options) limit(def int) int {
	switch {
	case o.Limit < 0:
		return -1
	case o.Limit == 0:
		return def
	}
	return o.Limit
}

// Navigator holds the documents to query.
type Navigator struct {
	docs []*markdown.Doc
	ids  *idpat.Matcher
}

// New returns a Navigator over docs, which should be in path order.
func New(docs []*markdown.Doc, ids *idpat.Matcher) *Navigator {
	return &Navigator{docs: docs, ids: ids}
}

// Line is one numbered source line.
type Line struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// Ref is one line that mentions something.
type Ref struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Where string `json:"where,omitzero"`
	Text  string `json:"text"`
}

// cleanCell strips emphasis, strike and code markers from a table cell.
func cleanCell(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "*_~`"))
}

// rowID returns the first cell of a table row when it is a known identifier.
func (n *Navigator) rowID(b markdown.Block) string {
	if b.Kind != markdown.KindTableRow || b.IsHeader || len(b.Cells) == 0 {
		return ""
	}
	c := cleanCell(b.Cells[0])
	if _, ok := n.ids.Classify(c); ok {
		return c
	}
	return ""
}

// isDefRow reports whether line ln of d is a table row whose first cell is id.
func isDefRow(d *markdown.Doc, ln int, id string) bool {
	b, ok := d.Smallest(ln)
	return ok && b.Kind == markdown.KindTableRow && !b.IsHeader &&
		len(b.Cells) > 0 && cleanCell(b.Cells[0]) == id
}

// headingTrail returns "A > B > C" for the sections enclosing line ln.
func headingTrail(d *markdown.Doc, ln int) string {
	return trailOf(d, d.HeadingAt(ln))
}

func trailOf(d *markdown.Doc, h int) string {
	var parts []string
	for _, i := range d.HeadingPath(h) {
		parts = append(parts, d.Headings[i].Text)
	}
	return strings.Join(parts, " > ")
}

// blockWhere is the breadcrumb for the smallest block holding line ln, such
// as "§ A > B > table row D334".
func (n *Navigator) blockWhere(d *markdown.Doc, ln int) string {
	desc := ""
	if b, ok := d.Smallest(ln); ok {
		desc = b.Kind.String()
		if id := n.rowID(b); id != "" {
			desc += " " + id
		}
	}
	return where(headingTrail(d, ln), desc)
}

// where builds the breadcrumb "§ A > B > desc".
func where(trail, desc string) string {
	switch {
	case trail == "" && desc == "":
		return ""
	case trail == "":
		return desc
	case desc == "":
		return "§ " + trail
	}
	return "§ " + trail + " > " + desc
}

func lines(d *markdown.Doc, from, to int) []Line {
	out := make([]Line, 0, to-from+1)
	for l := from; l <= to; l++ {
		out = append(out, Line{N: l, Text: d.Line(l)})
	}
	return out
}

// snippet returns line, or a window of it around [start, end) marked with "…"
// where text was left out.
func snippet(line string, start, end int, full bool) string {
	if full || len(line) <= snippetWidth {
		return line
	}
	pad := max((snippetWidth-(end-start))/2, 20)
	a, b := max(start-pad, 0), min(end+pad, len(line))
	for a > 0 && !utf8.RuneStart(line[a]) {
		a--
	}
	for b < len(line) && !utf8.RuneStart(line[b]) {
		b++
	}
	s := line[a:b]
	if a > 0 {
		s = "…" + s
	}
	if b < len(line) {
		s += "…"
	}
	return s
}
