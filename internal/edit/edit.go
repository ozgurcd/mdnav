// Package edit changes Markdown files block by block. Each operation
// addresses one block the way `mdnav get` does, re-finds it under the file's
// lock, checks the caller's expected hash, and splices new lines into the
// original bytes, so everything outside the block stays byte for byte.
package edit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ozgurcd/mdnav/internal/fileio"
	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
	"github.com/ozgurcd/mdnav/internal/nav"
)

// Operation names.
const (
	OpSub          = "sub"
	OpSet          = "set"
	OpReplace      = "replace"
	OpInsertBefore = "insert-before"
	OpInsertAfter  = "insert-after"
	OpDelete       = "delete"
	OpAddRow       = "add-row"
)

var (
	// ErrStale means the block changed since the caller read it: its hash no
	// longer equals --expect, or a table cell no longer holds --old.
	ErrStale = errors.New("block changed since it was read")
	// ErrNeedExpect means a whole-block overwrite came without --expect or --force.
	ErrNeedExpect = errors.New("needs --expect <hash> (from mdnav get) or --force")
	// ErrInvalid means the edit would break the document's structure or is malformed.
	ErrInvalid = errors.New("invalid edit")
)

// Op is one edit. It is also the JSON form `mdnav apply` reads.
type Op struct {
	Op     string   `json:"op"`
	Target string   `json:"target"`
	Expect string   `json:"expect,omitzero"` // block hash from get; a leading # is ignored
	Force  bool     `json:"force,omitzero"`  // replace/delete without --expect
	Old    *string  `json:"old,omitzero"`    // sub: exact text to replace; set: required current cell value
	New    string   `json:"new,omitzero"`    // sub: replacement text
	Column string   `json:"column,omitzero"` // set: header name of the cell's column
	Value  string   `json:"value,omitzero"`  // set: new cell value (plain text; pipes are escaped)
	Lines  []string `json:"lines,omitzero"`  // replace, insert-before, insert-after
	Cells  []string `json:"cells,omitzero"`  // add-row: plain cell values in column order
}

// Change describes what one operation did.
type Change struct {
	Op       string   `json:"op"`
	Target   string   `json:"target"`
	Where    string   `json:"where,omitzero"`
	OldStart int      `json:"old_start"`
	OldEnd   int      `json:"old_end"`
	OldHash  string   `json:"old_hash"` // hash of the addressed block before the edit
	Removed  []string `json:"removed,omitzero"`
	NewStart int      `json:"new_start,omitzero"`
	NewEnd   int      `json:"new_end,omitzero"`
	NewHash  string   `json:"new_hash,omitzero"` // pass as --expect to the next edit of this block
	NewWhere string   `json:"new_where,omitzero"`
	Added    []string `json:"added,omitzero"`
}

// Result is the outcome of Apply on one file.
type Result struct {
	Path    string   `json:"path"`
	DryRun  bool     `json:"dry_run,omitzero"`
	Written bool     `json:"written"`
	Changes []Change `json:"changes"`
}

// Apply runs ops in order on the file at path under one lock and writes the
// result once, or nothing if any op fails. With dryRun it reports the same
// changes and writes nothing.
func Apply(ctx context.Context, ids *idpat.Matcher, path string, ops []Op, dryRun bool) (Result, error) {
	res := Result{Path: path, DryRun: dryRun}
	if len(ops) == 0 {
		return res, fmt.Errorf("%w: no edits", ErrInvalid)
	}
	changed := false
	err := fileio.Update(ctx, path, !dryRun, func(raw []byte) ([]byte, error) {
		res.Changes = res.Changes[:0]
		cur := raw
		for i, op := range ops {
			c, next, err := applyOne(ctx, ids, path, cur, op)
			if err != nil {
				if len(ops) > 1 {
					err = fmt.Errorf("edit %d of %d (%s %q): %w", i+1, len(ops), op.Op, op.Target, err)
				}
				return nil, err
			}
			res.Changes = append(res.Changes, c)
			cur = next
		}
		changed = !bytes.Equal(cur, raw)
		return cur, nil
	})
	if err != nil {
		return Result{Path: path, DryRun: dryRun, Changes: []Change{}}, err
	}
	res.Written = changed && !dryRun
	return res, nil
}

func applyOne(ctx context.Context, ids *idpat.Matcher, path string, raw []byte, op Op) (Change, []byte, error) {
	doc := markdown.Parse(path, raw)
	t, err := nav.New([]*markdown.Doc{doc}, ids).Resolve(ctx, op.Target, nav.Options{})
	if err != nil {
		return Change{}, nil, err
	}
	c := Change{Op: op.Op, Target: op.Target, Where: t.Where, OldStart: t.Start, OldEnd: t.End, OldHash: t.Hash}
	if want := strings.TrimPrefix(strings.TrimSpace(op.Expect), "#"); want != "" && want != t.Hash {
		return c, nil, fmt.Errorf("%w: %s:%d-%d is #%s, not #%s; read it again with mdnav get",
			ErrStale, path, t.Start, t.End, t.Hash, want)
	}
	if (op.Op == OpReplace || op.Op == OpDelete) && op.Expect == "" && !op.Force {
		return c, nil, fmt.Errorf("%s %s:%d-%d: %w", op.Op, path, t.Start, t.End, ErrNeedExpect)
	}

	from, to := t.Start, t.End
	var lines []string
	switch op.Op {
	case OpSub:
		lines, err = substitute(doc, t, op)
	case OpSet:
		lines, err = setCell(doc, t, op)
	case OpReplace:
		if len(op.Lines) == 0 {
			err = fmt.Errorf("%w: replace needs new text; use delete to remove a block", ErrInvalid)
		}
		// Keep the blank lines that end a section's span (before the next heading).
		for to > from && isBlank(doc.Line(to)) {
			to--
		}
		lines = op.Lines
	case OpInsertBefore, OpInsertAfter:
		switch {
		case len(op.Lines) == 0:
			err = fmt.Errorf("%w: nothing to insert", ErrInvalid)
		case op.Op == OpInsertBefore && t.IsHeader:
			err = fmt.Errorf("%w: cannot insert before a table header row", ErrInvalid)
		}
		lines = op.Lines
		if op.Op == OpInsertBefore {
			to = from - 1
		} else {
			// After the block's last non-blank line: a section's span ends with the
			// blank lines before the next heading, which stay after the insert.
			for to > from && isBlank(doc.Line(to)) {
				to--
			}
			from = to + 1
		}
	case OpDelete:
		// A block between two blank lines takes one of them along, so the
		// blocks around it stay one blank line apart.
		if to < doc.NumLines() && isBlank(doc.Line(to+1)) && (from == 1 || isBlank(doc.Line(from-1))) {
			to++
		}
	case OpAddRow:
		from, lines, err = addRow(doc, t, op)
		to = from - 1
	default:
		err = fmt.Errorf("%w: unknown operation %q", ErrInvalid, op.Op)
	}
	if err != nil {
		return c, nil, err
	}
	for _, l := range lines {
		if strings.ContainsAny(l, "\r\n") {
			return c, nil, fmt.Errorf("%w: a line contains a line break", ErrInvalid)
		}
	}

	out := newText(raw).splice(from, to, lines)
	if to >= from {
		c.Removed = append([]string(nil), doc.Lines[from-1:to]...)
	}
	c.Added = lines
	newDoc := markdown.Parse(path, out)
	if err := validate(doc, newDoc, t, op, from, len(lines)); err != nil {
		return c, nil, err
	}
	if len(lines) > 0 {
		c.NewStart, c.NewEnd = from, from+len(lines)-1
		c.NewHash = nav.BlockHash(newDoc, c.NewStart, c.NewEnd)
		if nt, err := nav.New([]*markdown.Doc{newDoc}, ids).Resolve(ctx, fmt.Sprintf("%s:%d", path, from), nav.Options{}); err == nil {
			// Report the block a follow-up edit of this place would address.
			c.NewStart, c.NewEnd, c.NewHash, c.NewWhere = nt.Start, nt.End, nt.Hash, nt.Where
		}
	}
	return c, out, nil
}

// substitute replaces the one occurrence of op.Old in the block's text.
func substitute(doc *markdown.Doc, t nav.Target, op Op) ([]string, error) {
	if op.Old == nil || *op.Old == "" {
		return nil, fmt.Errorf("%w: sub needs the exact old text", ErrInvalid)
	}
	block := strings.Join(doc.Lines[t.Start-1:t.End], "\n")
	switch n := strings.Count(block, *op.Old); n {
	case 1:
	case 0:
		return nil, fmt.Errorf("%w: %q is not in %s:%d-%d (#%s)", ErrStale, *op.Old, doc.Path, t.Start, t.End, t.Hash)
	default:
		return nil, fmt.Errorf("%w: %q occurs %d times in %s:%d-%d; give more context so it occurs once",
			ErrInvalid, *op.Old, n, doc.Path, t.Start, t.End)
	}
	return strings.Split(strings.Replace(block, *op.Old, op.New, 1), "\n"), nil
}

// setCell changes one cell of a table row, keeping the cell's padding.
// rowForm names the row form of a set target: the row whose first cell is the
// target when there is one, else the generic form.
func rowForm(doc *markdown.Doc, target string) string {
	want := strings.TrimSpace(target)
	for _, tb := range doc.Tables {
		for n := tb.Sep + 1; n <= tb.End; n++ {
			if cells := markdown.SplitCells(doc.Line(n)); len(cells) > 0 && strings.EqualFold(strings.TrimSpace(cells[0]), want) {
				return fmt.Sprintf("%q", "| "+strings.TrimSpace(cells[0])+" |")
			}
		}
	}
	return `"| <col1> | <col2>"`
}

func setCell(doc *markdown.Doc, t nav.Target, op Op) ([]string, error) {
	if t.Table < 0 || t.IsHeader || t.Start != t.End {
		return nil, fmt.Errorf("%w: set needs a table body row; %s:%d-%d is a %s; address the row by its leading cells, as in %s",
			ErrInvalid, doc.Path, t.Start, t.End, t.Kind, rowForm(doc, op.Target))
	}
	headers := doc.Tables[t.Table].HeaderCells
	col := -1
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(op.Column)) {
			col = i
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("%w: no column %q; columns are %q", ErrInvalid, op.Column, headers)
	}
	line := doc.Line(t.Start)
	spans := markdown.CellSpans(line)
	if col >= len(spans) {
		return nil, fmt.Errorf("%w: row %s:%d has only %d cells", ErrInvalid, doc.Path, t.Start, len(spans))
	}
	s, e := spans[col][0], spans[col][1]
	cell := line[s:e]
	current := strings.ReplaceAll(strings.TrimSpace(cell), `\|`, "|")
	if op.Old != nil && current != *op.Old {
		return nil, fmt.Errorf("%w: %s:%d column %q is %q, not %q", ErrStale, doc.Path, t.Start, headers[col], current, *op.Old)
	}
	lead := cell[:len(cell)-len(strings.TrimLeft(cell, " \t"))]
	trail := cell[len(strings.TrimRight(cell, " \t")):]
	if strings.TrimSpace(cell) == "" {
		lead, trail = " ", " "
	}
	return []string{line[:s] + lead + escapePipes(op.Value) + trail + line[e:]}, nil
}

// addRow builds a new last row for the table the target is in, or the only
// table in the target section. It returns the line to insert before.
func addRow(doc *markdown.Doc, t nav.Target, op Op) (int, []string, error) {
	ti := t.Table
	if ti < 0 && t.IsSection {
		for i, tbl := range doc.Tables {
			if tbl.Header >= t.Start && tbl.Header <= t.End {
				if ti >= 0 {
					return 0, nil, fmt.Errorf("%w: section %s:%d-%d has more than one table; address a row of the one you want",
						ErrInvalid, doc.Path, t.Start, t.End)
				}
				ti = i
			}
		}
	}
	if ti < 0 {
		return 0, nil, fmt.Errorf("%w: %s:%d-%d is a %s, not a table or a section with one table",
			ErrInvalid, doc.Path, t.Start, t.End, t.Kind)
	}
	tbl := doc.Tables[ti]
	if len(op.Cells) != len(tbl.HeaderCells) {
		return 0, nil, fmt.Errorf("%w: %d cells given, the table has %d columns %q (escape a literal pipe as \\|)",
			ErrInvalid, len(op.Cells), len(tbl.HeaderCells), tbl.HeaderCells)
	}
	cells := make([]string, len(op.Cells))
	for i, v := range op.Cells {
		cells[i] = escapePipes(strings.TrimSpace(v))
	}
	return tbl.End + 1, []string{"| " + strings.Join(cells, " | ") + " |"}, nil
}

func escapePipes(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\|`, "|"), "|", `\|`)
}

// validate refuses results that break the document around the edit.
func validate(before, after *markdown.Doc, t nav.Target, op Op, from, n int) error {
	if !before.OpenFence() && after.OpenFence() {
		return fmt.Errorf("%w: the edit leaves a code fence unclosed", ErrInvalid)
	}
	// Lines that replace or sit next to a table row must be rows of that table,
	// or the table would be cut in two.
	rowEdit := t.Table >= 0 && op.Op != OpDelete
	for ln := from; ln < from+n; ln++ {
		b, ok := after.Smallest(ln)
		isRow := ok && b.Kind == markdown.KindTableRow
		if (rowEdit || op.Op == OpAddRow) && !isRow {
			return fmt.Errorf("%w: line %d would no longer be a table row", ErrInvalid, ln)
		}
		if isRow {
			if want := len(after.Tables[b.Table].HeaderCells); len(b.Cells) != want {
				return fmt.Errorf("%w: line %d would have %d cells, its table has %d columns (escape a literal pipe as \\|)",
					ErrInvalid, ln, len(b.Cells), want)
			}
		}
	}
	return nil
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }
