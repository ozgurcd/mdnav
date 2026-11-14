package nav

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/source"
)

func newNav(t *testing.T) *Navigator {
	t.Helper()
	docs, err := source.Load(t.Context(), []string{"testdata"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := idpat.New(idpat.Builtin())
	if err != nil {
		t.Fatal(err)
	}
	return New(docs, m)
}

func get(t *testing.T, n *Navigator, q string, o Options) GetResult {
	t.Helper()
	r, err := n.Get(t.Context(), q, o)
	if err != nil {
		t.Fatalf("Get(%q): %v", q, err)
	}
	return r
}

func texts(b Block) []string {
	var out []string
	for _, l := range b.Lines {
		out = append(out, l.Text)
	}
	return out
}

func TestGetID(t *testing.T) {
	n := newNav(t)
	r := get(t, n, "D4", Options{})
	if r.Match != MatchID || len(r.Blocks) != 1 {
		t.Fatalf("D4: match %s, %d blocks", r.Match, len(r.Blocks))
	}
	b := r.Blocks[0]
	if b.Path != "testdata/01-register.md" || b.Start != 10 || b.Kind != "table row" {
		t.Fatalf("D4 block: %+v", b)
	}
	want := []string{"| ID | Decision | Source |", "| D4 | Limits apply per lane. | Owner |"}
	if !slices.Equal(texts(b), want) {
		t.Fatalf("D4 lines: %q", texts(b))
	}
	if !strings.Contains(b.Where, "table row D4") {
		t.Errorf("where: %q", b.Where)
	}
	// The heading "Fence (D4)" is offered as see-also, not instead of the row.
	if len(r.SeeAlso) != 1 || r.SeeAlso[0].Line != 15 {
		t.Errorf("see also: %+v", r.SeeAlso)
	}
	// D4 must not return D44 and vice versa.
	r = get(t, n, "D44", Options{})
	if len(r.Blocks) != 1 || r.Blocks[0].Start != 12 {
		t.Fatalf("D44: %+v", r.Blocks)
	}
}

func TestGetIDMention(t *testing.T) {
	n := newNav(t)
	r := get(t, n, "M1.ABC-1", Options{})
	if r.Match != MatchIDMention && r.Match != MatchID {
		t.Fatalf("match: %s", r.Match)
	}
	// A struck first cell still defines the row.
	if len(r.Blocks) != 1 || r.Blocks[0].Start != 5 {
		t.Fatalf("blocks: %+v", r.Blocks)
	}
}

func TestGetSection(t *testing.T) {
	n := newNav(t)
	for _, q := range []string{"§5.4", "5.4", "§ 5.4"} {
		r := get(t, n, q, Options{})
		if r.Match != MatchSection || len(r.Blocks) != 1 {
			t.Fatalf("%s: match %s, %d blocks", q, r.Match, len(r.Blocks))
		}
		b := r.Blocks[0]
		// From "## 5.4 Loop" through the nested ### up to the line before "## 5.5".
		if b.Start != 11 || b.End != 21 || b.Lines[0].Text != "## 5.4 Loop" {
			t.Fatalf("%s: %d-%d %q", q, b.Start, b.End, b.Lines[0].Text)
		}
	}
}

func TestGetHeadingText(t *testing.T) {
	n := newNav(t)
	r := get(t, n, "scope", Options{})
	if r.Match != MatchHeading || len(r.Blocks) != 1 || r.Blocks[0].Start != 3 || r.Blocks[0].End != 10 {
		t.Fatalf("scope: %s %+v", r.Match, r.Blocks)
	}
}

func TestGetFreeText(t *testing.T) {
	n := newNav(t)
	r := get(t, n, "porting work", Options{})
	if r.Match != MatchText || len(r.Blocks) != 1 {
		t.Fatalf("match %s, blocks %+v", r.Match, r.Blocks)
	}
	b := r.Blocks[0]
	if b.Kind != "list item" || b.Start != 5 || b.End != 6 {
		t.Fatalf("list item: %+v", b)
	}
	// A match in a table row returns the row with its header, not the section.
	r = get(t, n, "pipe in text", Options{})
	if b := r.Blocks[0]; b.Kind != "table row" || len(b.Lines) != 2 || b.Start != 8 {
		t.Fatalf("row: %+v", b)
	}
	// Free text searches code blocks too.
	r = get(t, n, "inside code", Options{})
	if b := r.Blocks[0]; b.Kind != "code block" || b.Start != 17 || b.End != 20 {
		t.Fatalf("code: %+v", b)
	}
	// Case-insensitive.
	r = get(t, n, "blue widget", Options{})
	if len(r.Blocks) != 1 || r.Blocks[0].Kind != "paragraph" {
		t.Fatalf("case: %+v", r.Blocks)
	}
}

func TestGetLimitAndRegex(t *testing.T) {
	n := newNav(t)
	r := get(t, n, `sprockets? spin`, Options{Regex: true, Limit: 1})
	if len(r.Blocks) != 1 || r.More < 1 {
		t.Fatalf("limit: %d blocks, %d more", len(r.Blocks), r.More)
	}
	if _, err := n.Get(context.Background(), "(", Options{Regex: true}); err == nil {
		t.Fatal("bad regex should fail")
	}
	r = get(t, n, "no such phrase anywhere", Options{})
	if r.Match != MatchNone || len(r.Blocks) != 0 {
		t.Fatalf("none: %+v", r)
	}
}

func TestGetCapsLongBlocks(t *testing.T) {
	n := newNav(t)
	// The whole-file section is never printed; its outline is.
	r := get(t, n, "01. Register", Options{})
	b := r.Blocks[0]
	if len(b.Lines) != 0 || len(b.Outline) == 0 || !strings.Contains(b.Note, "whole file") {
		t.Fatalf("whole file: %+v", b)
	}
	// A section over the cap shows its outline and says so.
	r = get(t, n, "5.4", Options{MaxLines: 5})
	b = r.Blocks[0]
	if len(b.Lines) != 0 || !strings.Contains(b.Note, "over the 5-line cap") {
		t.Fatalf("section cap: %+v", b)
	}
	// A block over the cap is cut around the match and says so.
	r = get(t, n, "porting", Options{MaxLines: 1})
	b = r.Blocks[0]
	if len(b.Lines) != 1 || b.Lines[0].N != 6 || !strings.Contains(b.Note, "cut:") {
		t.Fatalf("block cap: %+v", b)
	}
}

func TestRefs(t *testing.T) {
	n := newNav(t)
	r, err := n.Refs(t.Context(), "D2", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.Files {
		for _, ref := range f.Refs {
			got = append(got, filepath.Base(f.Path)+":"+strconv.Itoa(ref.Line))
		}
	}
	// The defining row (01-register.md:8) and the code block line are excluded.
	want := []string{"01-register.md:3", "05-engine.md:8", "12-plan.md:6", "plain.md:2"}
	if !slices.Equal(got, want) {
		t.Fatalf("refs: %v, want %v", got, want)
	}
	if len(r.Excluded) != 1 || r.Excluded[0].Line != 8 {
		t.Fatalf("excluded: %+v", r.Excluded)
	}
	r, _ = n.Refs(t.Context(), "D2", Options{IncludeDef: true})
	if r.Total != 5 {
		t.Fatalf("include-def total: %d", r.Total)
	}
	r, _ = n.Refs(t.Context(), "D2", Options{Limit: 2})
	if r.Total != 4 || r.More != 2 {
		t.Fatalf("limit: total %d more %d", r.Total, r.More)
	}
}

func TestStatus(t *testing.T) {
	n := newNav(t)
	cases := []struct {
		id          string
		verdict     string
		lines       []string // file:line of hits
		superseders []string // of the first hit
	}{
		{"D1", VerdictPossibly, []string{"01-register.md:11"}, []string{"D5"}},
		{"D3", VerdictPossibly, []string{"01-register.md:9", "01-register.md:11", "05-engine.md:24", "12-plan.md:5"}, []string{"D5"}},
		{"D4", VerdictPossibly, []string{"12-plan.md:8"}, []string{"D5"}},
		{"M1.ABC-1", VerdictPossibly, []string{"12-plan.md:5"}, nil},
		{"D5", VerdictActive, nil, nil}, // only named as the superseder
		{"D2", VerdictActive, nil, nil},
		{"D99", VerdictNotFound, nil, nil},
	}
	for _, c := range cases {
		r, err := n.Status(t.Context(), c.id, Options{})
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, h := range r.Hits {
			lines = append(lines, filepath.Base(h.Path)+":"+strconv.Itoa(h.Line))
		}
		if r.Verdict != c.verdict || !slices.Equal(lines, c.lines) {
			t.Errorf("%s: %s %v; want %s %v", c.id, r.Verdict, lines, c.verdict, c.lines)
			continue
		}
		if len(r.Hits) > 0 && !slices.Equal(r.Hits[0].Superseders, c.superseders) {
			t.Errorf("%s superseders: %v, want %v", c.id, r.Hits[0].Superseders, c.superseders)
		}
	}
}

func TestOutline(t *testing.T) {
	n := newNav(t)
	r, err := n.Outline(t.Context(), "testdata/05-engine.md", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range r.Entries {
		got = append(got, e.Number+"|"+e.Text+"|"+strconv.Itoa(e.Start)+"-"+strconv.Itoa(e.End))
	}
	want := []string{"5|05. Engine|1-26", "5.1|5.1 Scope|3-10", "5.4|5.4 Loop|11-21", "|Fence (D4)|15-21", "5.5|5.5 After|22-26"}
	if !slices.Equal(got, want) {
		t.Fatalf("outline:\n%v\nwant\n%v", got, want)
	}
	r, _ = n.Outline(t.Context(), "testdata/01-register.md", Options{})
	if e := r.Entries[0]; e.Tables != 1 || e.Rows != 6 {
		t.Fatalf("table counts: %+v", e)
	}
	r, _ = n.Outline(t.Context(), "testdata/05-engine.md", Options{Depth: 1})
	if len(r.Entries) != 1 {
		t.Fatalf("depth 1: %+v", r.Entries)
	}
	r, _ = n.Outline(t.Context(), "testdata/plain.md", Options{})
	if len(r.Entries) != 0 || r.Note != "no headings" {
		t.Fatalf("no headings: %+v", r)
	}
	r, _ = n.Outline(t.Context(), "testdata/empty.md", Options{})
	if len(r.Entries) != 0 || r.Lines != 0 {
		t.Fatalf("empty: %+v", r)
	}
}

func TestIDs(t *testing.T) {
	n := newNav(t)
	r, err := n.IDs(t.Context(), Options{Pattern: "D"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range r.Entries {
		got = append(got, e.ID)
	}
	if want := []string{"D1", "D2", "D3", "D4", "D5", "D44"}; !slices.Equal(got, want) {
		t.Fatalf("ids: %v, want %v", got, want)
	}
	if _, err := n.IDs(t.Context(), Options{Pattern: "NOPE"}); err == nil {
		t.Fatal("unknown pattern should fail")
	}
}

func TestCancelledContext(t *testing.T) {
	n := newNav(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := n.Get(ctx, "porting", Options{}); err == nil {
		t.Fatal("Get should honour a cancelled context")
	}
}
