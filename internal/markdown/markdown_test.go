package markdown

import (
	"slices"
	"strings"
	"testing"
)

func parse(src string) *Doc { return Parse("t.md", []byte(src)) }

func TestEmptyFile(t *testing.T) {
	d := parse("")
	if d.NumLines() != 0 || len(d.Headings) != 0 || len(d.Blocks) != 0 {
		t.Fatalf("empty file: got %d lines, %d headings, %d blocks", d.NumLines(), len(d.Headings), len(d.Blocks))
	}
	if _, ok := d.Smallest(1); ok {
		t.Fatal("Smallest(1) on empty file should be false")
	}
	if d.HeadingAt(1) != -1 {
		t.Fatal("HeadingAt on empty file should be -1")
	}
}

func TestNoHeadings(t *testing.T) {
	d := parse("first paragraph\nstill first\n\nsecond paragraph\n")
	if len(d.Headings) != 0 {
		t.Fatalf("got %d headings", len(d.Headings))
	}
	b, ok := d.Smallest(2)
	if !ok || b.Kind != KindParagraph || b.Start != 1 || b.End != 2 {
		t.Fatalf("line 2: got %+v", b)
	}
	if d.HeadingAt(4) != -1 {
		t.Fatal("HeadingAt should be -1 without headings")
	}
}

func TestCRLF(t *testing.T) {
	d := parse("# 1. Title\r\n\r\n| ID | Text |\r\n|----|------|\r\n| X1 | a |\r\n")
	if d.NumLines() != 5 {
		t.Fatalf("lines: %d", d.NumLines())
	}
	for i, l := range d.Lines {
		if strings.Contains(l, "\r") {
			t.Fatalf("line %d keeps CR: %q", i+1, l)
		}
	}
	if len(d.Headings) != 1 || d.Headings[0].Text != "1. Title" || d.Headings[0].Number != "1" {
		t.Fatalf("heading: %+v", d.Headings)
	}
	b, _ := d.Smallest(5)
	if b.Kind != KindTableRow || !slices.Equal(b.Cells, []string{"X1", "a"}) {
		t.Fatalf("row: %+v", b)
	}
}

func TestHeadingLevelsEndSections(t *testing.T) {
	src := `# 3. Chapter
intro
## 3.1 First
a
### Detail
b
## 3.2 Second
c
# 4. Next chapter
d
`
	d := parse(src)
	want := []struct {
		text       string
		number     string
		start, end int
		parent     int
	}{
		{"3. Chapter", "3", 1, 8, -1},
		{"3.1 First", "3.1", 3, 6, 0},
		{"Detail", "", 5, 6, 1},
		{"3.2 Second", "3.2", 7, 8, 0},
		{"4. Next chapter", "4", 9, 10, -1},
	}
	if len(d.Headings) != len(want) {
		t.Fatalf("got %d headings", len(d.Headings))
	}
	for i, w := range want {
		h := d.Headings[i]
		if h.Text != w.text || h.Number != w.number || h.Line != w.start || h.End != w.end || h.Parent != w.parent {
			t.Errorf("heading %d: got %+v, want %+v", i, h, w)
		}
	}
	if got := d.HeadingAt(6); got != 2 {
		t.Errorf("HeadingAt(6) = %d, want 2", got)
	}
	if got := d.HeadingPath(2); !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("HeadingPath(2) = %v", got)
	}
}

func TestSectionNumbers(t *testing.T) {
	cases := map[string]string{
		"05. Control plane": "5",
		"5.4 Reconcile":     "5.4",
		"5.7.1 Evidence":    "5.7.1",
		"27a. Backends":     "27a",
		"v0.1.1. Smoke":     "",
		"2026-11-04 notes":  "",
		"Plain":             "",
	}
	for in, want := range cases {
		if got := SectionNumber(in); got != want {
			t.Errorf("SectionNumber(%q) = %q, want %q", in, got, want)
		}
	}
	queries := map[string]string{"§5.4": "5.4", "§ 05": "5", "5.4": "5.4", "05.": "5", "5.4x y": "", "D5": "", "§": ""}
	for in, want := range queries {
		got, ok := ParseSectionQuery(in)
		if got != want || ok != (want != "") {
			t.Errorf("ParseSectionQuery(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestEscapedPipes(t *testing.T) {
	cells := SplitCells(`| K1 | uses ` + "`a\\|b`" + ` here | x \| y |`)
	want := []string{"K1", "uses `a|b` here", "x | y"}
	if !slices.Equal(cells, want) {
		t.Fatalf("got %q, want %q", cells, want)
	}
	d := parse("| ID | Value |\n|---|---|\n| K1 | a \\| b |\n| K2 | c |\n")
	if len(d.Tables) != 1 || d.Tables[0].End != 4 {
		t.Fatalf("tables: %+v", d.Tables)
	}
	b, _ := d.Smallest(3)
	if len(b.Cells) != 2 || b.Cells[1] != "a | b" {
		t.Fatalf("row cells: %q", b.Cells)
	}
}

func TestNestedLists(t *testing.T) {
	src := `- top one
  continues here
  - nested a
    nested a continued
  - nested b
- top two

1. numbered
   - under numbered

   loose paragraph in numbered
2. second numbered
after list
`
	d := parse(src)
	check := func(line int, kind Kind, start, end int) {
		t.Helper()
		b, ok := d.Smallest(line)
		if !ok || b.Kind != kind || b.Start != start || b.End != end {
			t.Errorf("line %d: got %v %d-%d (ok=%v), want %v %d-%d", line, b.Kind, b.Start, b.End, ok, kind, start, end)
		}
	}
	check(1, KindListItem, 1, 5) // top item includes its nested lines
	check(2, KindListItem, 1, 5)
	check(4, KindListItem, 3, 4) // nested item with its continuation
	check(5, KindListItem, 5, 5)
	check(6, KindListItem, 6, 6)
	check(9, KindListItem, 9, 9)
	check(11, KindListItem, 8, 11) // loose paragraph belongs to its item
	check(12, KindListItem, 12, 13)
}

func TestFencedCode(t *testing.T) {
	src := "# 1. Title\n\n```text\n# not a heading\n| X1 | not a row |\n```\n\n~~~\n- not an item\n~~~~\nafter\n"
	d := parse(src)
	if len(d.Headings) != 1 {
		t.Fatalf("headings inside code were parsed: %+v", d.Headings)
	}
	if len(d.Tables) != 0 {
		t.Fatalf("tables inside code were parsed: %+v", d.Tables)
	}
	for _, ln := range []int{3, 4, 5, 6, 8, 9, 10} {
		if !d.InCode(ln) {
			t.Errorf("line %d should be code", ln)
		}
	}
	b, _ := d.Smallest(4)
	if b.Kind != KindCode || b.Start != 3 || b.End != 6 {
		t.Errorf("code block: %+v", b)
	}
	b, _ = d.Smallest(11)
	if b.Kind != KindParagraph {
		t.Errorf("line after fence: %+v", b)
	}
}

func TestFenceInsideListItem(t *testing.T) {
	src := "- item\n\n  ```\n  code\n  ```\n- next\n"
	d := parse(src)
	b, _ := d.Smallest(1)
	if b.Kind != KindListItem || b.End != 5 {
		t.Fatalf("item should include its fenced code: %+v", b)
	}
	b, _ = d.Smallest(4)
	if b.Kind != KindCode {
		t.Fatalf("code line: %+v", b)
	}
}

func TestBlockquote(t *testing.T) {
	d := parse("para\n\n> quoted one\n> quoted two\n\nend\n")
	b, _ := d.Smallest(4)
	if b.Kind != KindQuote || b.Start != 3 || b.End != 4 {
		t.Fatalf("quote: %+v", b)
	}
}

func TestStrikeSpans(t *testing.T) {
	line := "keep ~~old K1~~ superseded and ~~K2~~; tail ~~open"
	got := StrikeSpans(line)
	if len(got) != 2 || line[got[0][0]:got[0][1]] != "~~old K1~~" || line[got[1][0]:got[1][1]] != "~~K2~~" {
		t.Fatalf("spans: %v", got)
	}
}

func TestCellSpansMatchSplitCells(t *testing.T) {
	for _, line := range []string{
		"| D4 | Limits apply per lane. | Owner |",
		"|a|b|",
		"  | x \\| y |  z  |",
		"a | b | c",
		"| only |",
		"| trailing escaped \\|",
		"| | empty |",
	} {
		var got []string
		for _, s := range CellSpans(line) {
			got = append(got, strings.ReplaceAll(strings.TrimSpace(line[s[0]:s[1]]), `\|`, "|"))
		}
		if want := SplitCells(line); !slices.Equal(got, want) {
			t.Errorf("CellSpans(%q) cells %q, SplitCells %q", line, got, want)
		}
	}
}

func TestOpenFence(t *testing.T) {
	if parse("```\ncode\n```\n").OpenFence() {
		t.Error("closed fence reported open")
	}
	if !parse("text\n```go\ncode\n").OpenFence() {
		t.Error("unclosed fence not reported")
	}
	if parse("").OpenFence() {
		t.Error("empty file reported open")
	}
}
