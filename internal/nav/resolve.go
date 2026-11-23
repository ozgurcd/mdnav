package nav

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// Target is the one block an edit addresses.
type Target struct {
	Path       string `json:"path"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Kind       string `json:"kind"` // "table row", "table header", "section", "list item", ...
	Where      string `json:"where,omitzero"`
	Hash       string `json:"hash"`
	Table      int    `json:"-"` // index into the document's Tables for a table row or header, else -1
	IsHeader   bool   `json:"-"`
	IsSection  bool   `json:"-"`
	HeaderLine int    `json:"-"` // the table's header row line, for table rows
}

// ErrNoTarget is returned when no block matches an edit target.
var ErrNoTarget = errors.New("no block matches")

// AmbiguousError is returned when an edit target matches more than one block.
type AmbiguousError struct {
	Query      string
	Candidates []string // "path:start-end (where) #hash"
	More       int
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches more than one block; give a more exact target or path:line:", e.Query)
	for _, c := range e.Candidates {
		b.WriteString("\n  " + c)
	}
	if e.More > 0 {
		fmt.Fprintf(&b, "\n  ... %d more", e.More)
	}
	return b.String()
}

// maxCandidates bounds the list in an AmbiguousError.
const maxCandidates = 10

var lineTargetRe = regexp.MustCompile(`^(.+):([0-9]+)$`)

// Resolve finds the single block an edit addresses. q is anything get accepts
// (id, § number, heading text, free text) or path:line, which addresses the
// smallest block containing that line, or the whole section for a heading line.
func (n *Navigator) Resolve(ctx context.Context, q string, o Options) (Target, error) {
	targets, _, _, err := n.find(ctx, q, o)
	switch {
	case err != nil:
		return Target{}, err
	case len(targets) == 0:
		return Target{}, fmt.Errorf("%w: %q", ErrNoTarget, q)
	case len(targets) == 1:
		return n.toTarget(targets[0]), nil
	}
	amb := &AmbiguousError{Query: q}
	for i, t := range targets {
		if i == maxCandidates {
			amb.More = len(targets) - maxCandidates
			break
		}
		x := n.toTarget(t)
		amb.Candidates = append(amb.Candidates, fmt.Sprintf("%s:%d-%d (%s) #%s", x.Path, x.Start, x.End, x.Where, x.Hash))
	}
	return Target{}, amb
}

func (n *Navigator) toTarget(t target) Target {
	r := n.render(t, 1) // only the header fields are used
	out := Target{Path: r.Path, Start: r.Start, End: r.End, Kind: r.Kind, Where: r.Where, Hash: r.Hash,
		Table: -1, IsSection: t.section >= 0}
	if t.section < 0 && t.block.Kind == markdown.KindTableRow {
		out.Table, out.IsHeader = t.block.Table, t.block.IsHeader
		out.HeaderLine = t.doc.Tables[t.block.Table].Header
	}
	return out
}

// lineTarget handles a path:line query naming a loaded document. It reports
// false when q is not of that form or names no loaded document.
func (n *Navigator) lineTarget(q string) (target, bool, error) {
	m := lineTargetRe.FindStringSubmatch(strings.TrimSpace(q))
	if m == nil {
		return target{}, false, nil
	}
	path := filepath.ToSlash(filepath.Clean(m[1]))
	for _, d := range n.docs {
		if d.Path != path {
			continue
		}
		ln, err := strconv.Atoi(m[2])
		if err != nil || ln < 1 || ln > d.NumLines() {
			return target{}, true, fmt.Errorf("%s: line %s is outside the file (1-%d)", d.Path, m[2], d.NumLines())
		}
		b, ok := d.Smallest(ln)
		if !ok {
			return target{}, true, fmt.Errorf("%w: %s:%d is blank or a table separator", ErrNoTarget, d.Path, ln)
		}
		if b.Kind == markdown.KindHeading {
			return sectionTarget(d, b.Heading), true, nil
		}
		return target{doc: d, block: b, section: -1, focus: ln}, true, nil
	}
	return target{}, false, nil
}
