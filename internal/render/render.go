// Package render writes navigation results as plain text or JSON.
//
// Text output is meant for agents: every block starts with a gograph-style
// header line "// path:start-end (where)", and every cut says so.
package render

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"strings"

	"github.com/ozgurcd/mdnav/internal/nav"
)

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	if err := json.MarshalWrite(w, v, jsontext.WithIndent("  ")); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func span(start, end int) string {
	if start == end {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func header(path string, start, end int, where string) string {
	h := fmt.Sprintf("// %s:%s", path, span(start, end))
	if where != "" {
		h += " (" + where + ")"
	}
	return h
}

// Get writes a get result.
func Get(w io.Writer, r nav.GetResult) error {
	var b strings.Builder
	if len(r.Blocks) == 0 {
		fmt.Fprintf(&b, "// no match for %q\n", r.Query)
	}
	for i, blk := range r.Blocks {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(header(blk.Path, blk.Start, blk.End, blk.Where) + "\n")
		if blk.Note != "" {
			b.WriteString("// " + blk.Note + "\n")
		}
		for _, l := range blk.Lines {
			b.WriteString(l.Text + "\n")
		}
		writeOutline(&b, blk.Outline)
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "\n... %d more matches (use --limit)\n", r.More)
	}
	if len(r.SeeAlso) > 0 {
		b.WriteString("\n// see also (headings and other table rows with this id):\n")
		for _, ref := range r.SeeAlso {
			fmt.Fprintf(&b, "//   %s:%d %s\n", ref.Path, ref.Line, ref.Text)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeOutline(b *strings.Builder, entries []nav.OutlineEntry) {
	if len(entries) == 0 {
		return
	}
	base := entries[0].Level
	for _, e := range entries {
		base = min(base, e.Level)
	}
	for _, e := range entries {
		indent := strings.Repeat("  ", e.Level-base)
		line := fmt.Sprintf("%s%s %s  %s", indent, strings.Repeat("#", e.Level), e.Text, span(e.Start, e.End))
		if e.Number != "" {
			line += "  §" + e.Number
		}
		if e.Tables > 0 {
			line += fmt.Sprintf("  [tables %d, rows %d]", e.Tables, e.Rows)
		}
		b.WriteString(line + "\n")
	}
}

// Outline writes an outline result.
func Outline(w io.Writer, r nav.OutlineResult) error {
	var b strings.Builder
	end := max(r.Lines, 1)
	b.WriteString(header(r.Path, 1, end, "outline") + "\n")
	if r.Note != "" {
		b.WriteString("// " + r.Note + "\n")
	}
	writeOutline(&b, r.Entries)
	if r.More > 0 {
		fmt.Fprintf(&b, "... %d more headings\n", r.More)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Refs writes a refs result.
func Refs(w io.Writer, r nav.RefsResult) error {
	var b strings.Builder
	if r.Total == 0 {
		fmt.Fprintf(&b, "// no references to %s\n", r.ID)
	}
	for i, f := range r.Files {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "// %s (%d)\n", f.Path, len(f.Refs))
		writeRefs(&b, f.Refs)
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "\n... %d more references (use --limit)\n", r.More)
	}
	if len(r.Excluded) > 0 {
		b.WriteString("\n// defining rows left out (use --include-def):")
		for _, e := range r.Excluded {
			fmt.Fprintf(&b, " %s:%d", e.Path, e.Line)
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeRefs(b *strings.Builder, refs []nav.Ref) {
	for _, ref := range refs {
		if ref.Where != "" {
			fmt.Fprintf(b, "%d: [§ %s] %s\n", ref.Line, ref.Where, ref.Text)
		} else {
			fmt.Fprintf(b, "%d: %s\n", ref.Line, ref.Text)
		}
	}
}

// Status writes a status result.
func Status(w io.Writer, r nav.StatusResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", r.ID, r.Summary)
	b.WriteString("// hint only: drawn from supersession wording and struck-through text, not a ruling\n")
	for _, d := range r.Defined {
		fmt.Fprintf(&b, "// defined at %s:%d\n", d.Path, d.Line)
	}
	for _, h := range r.Hits {
		b.WriteString("\n" + header(h.Path, h.Line, h.Line, h.Where) + "\n")
		line := "// " + h.Reason
		if len(h.Superseders) > 0 {
			line += "; superseding id on this line: " + strings.Join(h.Superseders, ", ")
		}
		b.WriteString(line + "\n" + h.Text + "\n")
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "\n... %d more lines (use --limit)\n", r.More)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// IDs writes an ids result.
func IDs(w io.Writer, r nav.IDsResult) error {
	var b strings.Builder
	if r.Total == 0 {
		b.WriteString("// no identifiers found\n")
	}
	for _, e := range r.Entries {
		def := "-"
		if len(e.Defined) > 0 {
			def = strings.Join(e.Defined, ",")
		}
		fmt.Fprintf(&b, "%s\t%s\tmentions=%d files=%d\tdefined=%s\n", e.ID, e.Pattern, e.Mentions, e.Files, def)
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "... %d more ids (use --limit)\n", r.More)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
