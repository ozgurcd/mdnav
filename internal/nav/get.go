package nav

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// How a get query was resolved.
const (
	MatchID        = "id"         // defining table row or heading
	MatchIDMention = "id-mention" // no definition; blocks that mention the id
	MatchSection   = "section"    // § number
	MatchHeading   = "heading"    // heading text
	MatchText      = "text"       // free text
	MatchNone      = "none"
)

// Block is one printed block: lines plus a header saying where they are.
// When a section is too long, Lines is empty and Outline lists its headings.
type Block struct {
	Path    string         `json:"path"`
	Start   int            `json:"start"`
	End     int            `json:"end"`
	Kind    string         `json:"kind"`
	Where   string         `json:"where,omitzero"`
	Lines   []Line         `json:"lines,omitzero"`
	Outline []OutlineEntry `json:"outline,omitzero"`
	Note    string         `json:"note,omitzero"`
}

// GetResult is the answer to a get query.
type GetResult struct {
	Query   string  `json:"query"`
	Match   string  `json:"match"`
	Blocks  []Block `json:"blocks"`
	More    int     `json:"more,omitzero"`
	SeeAlso []Ref   `json:"see_also,omitzero"`
}

// target is a block or section chosen before rendering.
type target struct {
	doc     *markdown.Doc
	block   markdown.Block
	section int    // heading index when the target is a whole section, else -1
	focus   int    // matched line, used to centre a cut block
	label   string // overrides the default description
}

// Get resolves q in this order: exact identifier, § number, heading text,
// free text. The first step that finds anything wins.
func (n *Navigator) Get(ctx context.Context, q string, o Options) (GetResult, error) {
	res := GetResult{Query: q, Match: MatchNone}
	q = strings.TrimSpace(q)
	if q == "" {
		return res, fmt.Errorf("empty query")
	}
	var targets []target
	var err error
	if !o.Regex {
		if _, ok := n.ids.Classify(q); ok {
			targets, res.SeeAlso, err = n.byID(ctx, q)
			res.Match = MatchID
			if err == nil && len(targets) == 0 {
				targets, err = n.byText(ctx, n.idLineMatcher(q), true)
				res.Match = MatchIDMention
			}
		}
		if err == nil && len(targets) == 0 {
			if num, ok := markdown.ParseSectionQuery(q); ok {
				targets, err = n.bySection(ctx, num)
				res.Match = MatchSection
			}
		}
		if err == nil && len(targets) == 0 {
			targets, err = n.byHeading(ctx, q)
			res.Match = MatchHeading
		}
	}
	if err == nil && len(targets) == 0 {
		var match func(string) bool
		match, err = textMatcher(q, o.Regex)
		if err == nil {
			targets, err = n.byText(ctx, match, false)
			res.Match = MatchText
		}
	}
	if err != nil {
		return res, err
	}
	if len(targets) == 0 {
		res.Match = MatchNone
	}
	if lim := o.limit(DefaultGetLimit); lim >= 0 && len(targets) > lim {
		res.More = len(targets) - lim
		targets = targets[:lim]
	}
	res.Blocks = make([]Block, 0, len(targets))
	for _, t := range targets {
		res.Blocks = append(res.Blocks, n.render(t, o.maxLines()))
	}
	return res, nil
}

func textMatcher(q string, isRegex bool) (func(string) bool, error) {
	if isRegex {
		re, err := regexp.Compile("(?i)" + q)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	}
	lq := strings.ToLower(q)
	return func(s string) bool { return strings.Contains(strings.ToLower(s), lq) }, nil
}

func (n *Navigator) idLineMatcher(id string) func(string) bool {
	return func(s string) bool { return n.ids.Contains(s, id) }
}

// byID finds table rows whose first cell is id. Without such rows it falls
// back to headings that carry the id. Headings that carry the id next to a
// defining row are returned as see-also references.
func (n *Navigator) byID(ctx context.Context, id string) ([]target, []Ref, error) {
	var rows, heads []target
	var also []Ref
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		for _, b := range d.Blocks {
			switch {
			case b.Kind == markdown.KindTableRow && !b.IsHeader && len(b.Cells) > 0 && cleanCell(b.Cells[0]) == id:
				rows = append(rows, target{doc: d, block: b, section: -1, focus: b.Start})
			case b.Kind == markdown.KindHeading && n.ids.Contains(d.Headings[b.Heading].Text, id):
				heads = append(heads, target{doc: d, block: b, section: b.Heading, focus: b.Start})
				also = append(also, Ref{Path: d.Path, Line: b.Start, Where: headingTrail(d, b.Start), Text: d.Line(b.Start)})
			}
		}
	}
	if len(rows) > 0 {
		best, others := n.preferRegister(rows, id)
		for _, t := range others {
			also = append(also, Ref{Path: t.doc.Path, Line: t.block.Start, Where: "other row with this id",
				Text: snippet(t.doc.Line(t.block.Start), 0, 0, false)})
		}
		return best, also, nil
	}
	return heads, nil, nil
}

// preferRegister keeps the rows whose table defines the most identifiers of
// id's pattern (the register) and returns the rest separately. A summary
// table that repeats a few ids loses to the register that defines them all.
func (n *Navigator) preferRegister(rows []target, id string) (best, others []target) {
	pattern, _ := n.ids.Classify(id)
	score := func(t target) int {
		c := 0
		for _, b := range t.doc.Blocks {
			if b.Table == t.block.Table && b.Kind == markdown.KindTableRow {
				if p, ok := n.ids.Classify(cleanCell(firstCell(b))); ok && p == pattern {
					c++
				}
			}
		}
		return c
	}
	top := -1
	scores := make([]int, len(rows))
	for i, t := range rows {
		scores[i] = score(t)
		top = max(top, scores[i])
	}
	for i, t := range rows {
		if scores[i] == top {
			best = append(best, t)
		} else {
			others = append(others, t)
		}
	}
	return best, others
}

func firstCell(b markdown.Block) string {
	if len(b.Cells) == 0 || b.IsHeader {
		return ""
	}
	return b.Cells[0]
}

func (n *Navigator) bySection(ctx context.Context, num string) ([]target, error) {
	var out []target
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i, h := range d.Headings {
			if h.Number == num {
				out = append(out, sectionTarget(d, i))
			}
		}
	}
	return out, nil
}

func (n *Navigator) byHeading(ctx context.Context, q string) ([]target, error) {
	lq := strings.ToLower(q)
	var out []target
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for i, h := range d.Headings {
			if strings.Contains(strings.ToLower(h.Text), lq) {
				out = append(out, sectionTarget(d, i))
			}
		}
	}
	return out, nil
}

func sectionTarget(d *markdown.Doc, h int) target {
	hd := d.Headings[h]
	return target{doc: d, section: h, focus: hd.Line,
		block: markdown.Block{Kind: markdown.KindHeading, Start: hd.Line, End: hd.End, Table: -1, Heading: h}}
}

// byText returns the smallest enclosing block of every matching line, once
// per block, in file order. skipCode excludes fenced code (used for ids).
func (n *Navigator) byText(ctx context.Context, match func(string) bool, skipCode bool) ([]target, error) {
	var out []target
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lastStart, lastEnd := 0, 0
		for ln := 1; ln <= d.NumLines(); ln++ {
			if skipCode && d.InCode(ln) || !match(d.Line(ln)) {
				continue
			}
			b, ok := d.Smallest(ln)
			if !ok {
				continue // e.g. a table separator row
			}
			if b.Kind == markdown.KindHeading {
				t := sectionTarget(d, b.Heading)
				b = t.block
				if b.Start == lastStart && b.End == lastEnd {
					continue
				}
				out = append(out, t)
			} else {
				if b.Start == lastStart && b.End == lastEnd {
					continue
				}
				out = append(out, target{doc: d, block: b, section: -1, focus: ln})
			}
			lastStart, lastEnd = b.Start, b.End
		}
	}
	return out, nil
}

// render turns a target into printable lines, applying the line cap.
func (n *Navigator) render(t target, maxLines int) Block {
	d, b := t.doc, t.block
	out := Block{Path: d.Path, Start: b.Start, End: b.End}
	if t.section >= 0 {
		return renderSection(d, t.section, maxLines)
	}
	trail := headingTrail(d, b.Start)
	switch b.Kind {
	case markdown.KindTableRow:
		tbl := d.Tables[b.Table]
		out.Kind = "table row"
		desc := "table row"
		if id := n.rowID(b); id != "" {
			desc += " " + id
		}
		if b.IsHeader {
			out.Kind, desc = "table header", "table header"
			out.Lines = lines(d, b.Start, b.End)
		} else {
			out.Lines = append(lines(d, tbl.Header, tbl.Header), lines(d, b.Start, b.End)...)
		}
		out.Where = where(trail, desc)
		return out
	default:
		out.Kind = b.Kind.String()
		out.Where = where(trail, out.Kind)
	}
	from, to := b.Start, b.End
	if size := to - from + 1; size > maxLines {
		from = max(b.Start, min(t.focus-maxLines/2, b.End-maxLines+1))
		to = from + maxLines - 1
		out.Note = fmt.Sprintf("cut: %s is %d lines (%d-%d), over the %d-line cap; showing %d-%d",
			out.Kind, size, b.Start, b.End, maxLines, from, to)
	}
	out.Lines = lines(d, from, to)
	return out
}

func renderSection(d *markdown.Doc, h, maxLines int) Block {
	hd := d.Headings[h]
	out := Block{Path: d.Path, Start: hd.Line, End: hd.End, Kind: "section", Where: where(trailOf(d, h), "")}
	size := hd.End - hd.Line + 1
	first, last := nonBlankRange(d)
	switch {
	case hd.Line <= first && hd.End >= last && d.NumLines() > 1:
		out.Note = fmt.Sprintf("section spans the whole file (%d lines); showing its outline instead", size)
	case size > maxLines:
		out.Note = fmt.Sprintf("section is %d lines (%d-%d), over the %d-line cap; showing its outline instead",
			size, hd.Line, hd.End, maxLines)
	default:
		out.Lines = lines(d, hd.Line, hd.End)
		return out
	}
	out.Outline = outlineEntries(d, hd.Line, hd.End, 0)
	if len(out.Outline) > maxLines {
		out.Note += fmt.Sprintf("; outline cut to %d of %d headings", maxLines, len(out.Outline))
		out.Outline = out.Outline[:maxLines]
	}
	return out
}

func nonBlankRange(d *markdown.Doc) (first, last int) {
	for l := 1; l <= d.NumLines(); l++ {
		if strings.TrimSpace(d.Line(l)) != "" {
			if first == 0 {
				first = l
			}
			last = l
		}
	}
	return first, last
}
