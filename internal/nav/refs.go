package nav

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
)

// RefFile groups the references in one file.
type RefFile struct {
	Path string `json:"path"`
	Refs []Ref  `json:"refs"`
}

// RefsResult lists every line that mentions an identifier.
type RefsResult struct {
	ID       string    `json:"id"`
	Files    []RefFile `json:"files"`
	Total    int       `json:"total"`
	More     int       `json:"more,omitzero"`
	Excluded []Ref     `json:"excluded_definitions,omitzero"`
}

// Refs lists the lines that mention id, outside fenced code, grouped by file.
// Defining rows are left out unless o.IncludeDef.
func (n *Navigator) Refs(ctx context.Context, id string, o Options) (RefsResult, error) {
	res := RefsResult{ID: id, Files: []RefFile{}}
	lim := o.limit(DefaultListLimit)
	shown := 0
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var f RefFile
		for ln := 1; ln <= d.NumLines(); ln++ {
			if d.InCode(ln) {
				continue
			}
			hits := n.ids.Find(d.Line(ln), id)
			if len(hits) == 0 {
				continue
			}
			ref := Ref{Path: d.Path, Line: ln, Where: headingTrail(d, ln), Hash: spanHash(d, ln), Text: snippet(d.Line(ln), hits[0][0], hits[0][1], o.Full)}
			if !o.IncludeDef && isDefRow(d, ln, id) {
				ref.Text = snippet(d.Line(ln), hits[0][0], hits[0][1], false)
				res.Excluded = append(res.Excluded, ref)
				continue
			}
			res.Total++
			if lim >= 0 && shown >= lim {
				res.More++
				continue
			}
			shown++
			f.Refs = append(f.Refs, ref)
		}
		if len(f.Refs) > 0 {
			f.Path = d.Path
			res.Files = append(res.Files, f)
		}
	}
	return res, nil
}

// IDEntry describes one identifier found in the documents.
type IDEntry struct {
	ID       string   `json:"id"`
	Pattern  string   `json:"pattern"`
	Defined  []string `json:"defined,omitzero"` // "path:line" of defining rows or headings
	Mentions int      `json:"mentions"`         // lines that mention it, definitions included
	Files    int      `json:"files"`
}

// IDsResult lists identifiers.
type IDsResult struct {
	Entries []IDEntry `json:"entries"`
	Total   int       `json:"total"`
	More    int       `json:"more,omitzero"`
}

// IDs lists every identifier outside fenced code, sorted by pattern order and
// then naturally (D2 before D10). o.Pattern limits the list to one pattern.
func (n *Navigator) IDs(ctx context.Context, o Options) (IDsResult, error) {
	order := make(map[string]int)
	for i, p := range n.ids.Patterns() {
		order[p.Name] = i
	}
	if _, ok := order[o.Pattern]; o.Pattern != "" && !ok {
		var names []string
		for _, p := range n.ids.Patterns() {
			names = append(names, p.Name)
		}
		return IDsResult{}, fmt.Errorf("unknown pattern %q (known: %s)", o.Pattern, strings.Join(names, ", "))
	}
	type acc struct {
		IDEntry
		files map[string]bool
	}
	byID := make(map[string]*acc)
	for _, d := range n.docs {
		if err := ctx.Err(); err != nil {
			return IDsResult{}, err
		}
		for ln := 1; ln <= d.NumLines(); ln++ {
			if d.InCode(ln) {
				continue
			}
			seen := make(map[string]bool)
			for _, m := range n.ids.FindAll(d.Line(ln)) {
				if o.Pattern != "" && m.Pattern != o.Pattern || seen[m.ID] {
					continue
				}
				seen[m.ID] = true
				a := byID[m.ID]
				if a == nil {
					a = &acc{IDEntry: IDEntry{ID: m.ID, Pattern: m.Pattern}, files: make(map[string]bool)}
					byID[m.ID] = a
				}
				a.Mentions++
				a.files[d.Path] = true
				if isDefRow(d, ln, m.ID) || isHeadingLine(d, ln) {
					a.Defined = append(a.Defined, fmt.Sprintf("%s:%d", d.Path, ln))
				}
			}
		}
	}
	entries := make([]IDEntry, 0, len(byID))
	for _, a := range byID {
		a.Files = len(a.files)
		entries = append(entries, a.IDEntry)
	}
	slices.SortFunc(entries, func(x, y IDEntry) int {
		return cmp.Or(cmp.Compare(order[x.Pattern], order[y.Pattern]), idpat.Compare(x.ID, y.ID))
	})
	res := IDsResult{Total: len(entries), Entries: entries}
	if lim := o.limit(DefaultListLimit); lim >= 0 && len(entries) > lim {
		res.More = len(entries) - lim
		res.Entries = entries[:lim]
	}
	return res, nil
}

func isHeadingLine(d *markdown.Doc, ln int) bool {
	b, ok := d.Smallest(ln)
	return ok && b.Kind == markdown.KindHeading
}
