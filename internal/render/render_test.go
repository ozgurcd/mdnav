package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ozgurcd/mdnav/internal/nav"
)

func TestGetHeaderAndNotes(t *testing.T) {
	r := nav.GetResult{
		Query: "K1",
		Blocks: []nav.Block{
			{Path: "d/a.md", Start: 9, End: 9, Kind: "table row", Where: "§ A > table row K1",
				Lines: []nav.Line{{N: 3, Text: "| ID | X |"}, {N: 9, Text: "| K1 | y |"}}},
			{Path: "d/b.md", Start: 1, End: 400, Kind: "section", Where: "§ B",
				Note:    "section is 400 lines (1-400), over the 200-line cap; showing its outline instead",
				Outline: []nav.OutlineEntry{{Level: 1, Number: "2", Text: "2. B", Start: 1, End: 400, Tables: 1, Rows: 3}}},
		},
		More: 4,
	}
	var buf bytes.Buffer
	if err := Get(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"// d/a.md:9 (§ A > table row K1)\n| ID | X |\n| K1 | y |\n",
		"// d/b.md:1-400 (§ B)\n// section is 400 lines",
		"# 2. B  1-400  §2  [tables 1, rows 3]",
		"... 4 more matches",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestNoMatch(t *testing.T) {
	var buf bytes.Buffer
	if err := Get(&buf, nav.GetResult{Query: "zz"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `no match for "zz"`) {
		t.Fatal(buf.String())
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, nav.Ref{Path: "a.md", Line: 2, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"path\": \"a.md\",\n  \"line\": 2,\n  \"text\": \"x\"\n}\n"
	if buf.String() != want {
		t.Fatalf("got %q", buf.String())
	}
}
