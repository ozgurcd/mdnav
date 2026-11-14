package nav

import (
	"context"
	"fmt"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// OutlineEntry is one heading with its section's line range. Tables and Rows
// count the tables and body rows directly in the section, before its first
// subsection.
type OutlineEntry struct {
	Level  int    `json:"level"`
	Number string `json:"number,omitzero"`
	Text   string `json:"text"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Tables int    `json:"tables,omitzero"`
	Rows   int    `json:"rows,omitzero"`
}

// OutlineResult is the heading tree of one file.
type OutlineResult struct {
	Path    string         `json:"path"`
	Lines   int            `json:"lines"`
	Entries []OutlineEntry `json:"entries"`
	More    int            `json:"more,omitzero"`
	Note    string         `json:"note,omitzero"`
}

// Outline returns the heading tree of the document at path, which must be
// one of the loaded documents.
func (n *Navigator) Outline(ctx context.Context, path string, o Options) (OutlineResult, error) {
	if err := ctx.Err(); err != nil {
		return OutlineResult{}, err
	}
	var d *markdown.Doc
	for _, x := range n.docs {
		if x.Path == path {
			d = x
		}
	}
	if d == nil {
		return OutlineResult{}, fmt.Errorf("%s: not loaded", path)
	}
	res := OutlineResult{Path: d.Path, Lines: d.NumLines()}
	all := outlineEntries(d, 1, d.NumLines(), o.Depth)
	if cap := o.maxLines(); len(all) > cap {
		res.More = len(all) - cap
		res.Note = fmt.Sprintf("outline cut: %d headings, showing the first %d (use --depth or --max-lines)", len(all), cap)
		all = all[:cap]
	}
	res.Entries = all
	if res.Entries == nil {
		res.Entries = []OutlineEntry{}
		res.Note = "no headings"
	}
	return res, nil
}

// outlineEntries lists headings that start within [from, to], at most depth
// deep (0 = all).
func outlineEntries(d *markdown.Doc, from, to, depth int) []OutlineEntry {
	var out []OutlineEntry
	for i, h := range d.Headings {
		if h.Line < from || h.Line > to || depth > 0 && h.Level > depth {
			continue
		}
		direct := d.NumLines()
		if i+1 < len(d.Headings) {
			direct = d.Headings[i+1].Line - 1
		}
		e := OutlineEntry{Level: h.Level, Number: h.Number, Text: h.Text, Start: h.Line, End: h.End}
		for _, t := range d.Tables {
			if t.Header >= h.Line && t.Header <= direct {
				e.Tables++
				e.Rows += t.End - t.Sep
			}
		}
		out = append(out, e)
	}
	return out
}
