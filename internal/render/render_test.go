package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ozgurcd/mdnav/internal/edit"
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

func TestHashesInTextOutput(t *testing.T) {
	var buf bytes.Buffer
	err := Get(&buf, nav.GetResult{
		Blocks:  []nav.Block{{Path: "a.md", Start: 3, End: 3, Where: "§ A > paragraph", Hash: "0123456789ab", Lines: []nav.Line{{N: 3, Text: "x"}}}},
		SeeAlso: []nav.Ref{{Path: "b.md", Line: 7, Hash: "ba9876543210", Text: "## B (K1)"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"// a.md:3 (§ A > paragraph) #0123456789ab\nx\n",
		"//   b.md:7 #ba9876543210 ## B (K1)\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in:\n%s", want, buf.String())
		}
	}

	buf.Reset()
	err = Refs(&buf, nav.RefsResult{ID: "K1", Total: 2, Files: []nav.RefFile{{Path: "a.md", Refs: []nav.Ref{
		{Line: 4, Where: "A", Hash: "0123456789ab", Text: "see K1"},
		{Line: 9, Text: "no heading K1"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "4 #0123456789ab: [§ A] see K1\n9: no heading K1\n"; !strings.Contains(buf.String(), want) {
		t.Errorf("refs output:\n%s\nwant to contain:\n%s", buf.String(), want)
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

func TestEditOutput(t *testing.T) {
	var buf bytes.Buffer
	err := Edit(&buf, edit.Result{Path: "a.md", Written: true, Changes: []edit.Change{
		{Op: "sub", OldStart: 9, OldEnd: 9, OldHash: "aaaaaaaaaaaa", Removed: []string{"| K | a |"},
			NewStart: 9, NewEnd: 9, NewHash: "bbbbbbbbbbbb", NewWhere: "§ A > table row K", Added: []string{"| K | b |"}},
		{Op: "delete", OldStart: 12, OldEnd: 12, OldHash: "cccccccccccc", Where: "§ A > paragraph", Removed: []string{"gone"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "// a.md:9 (§ A > table row K) #bbbbbbbbbbbb  [sub of #aaaaaaaaaaaa]\n-| K | a |\n+| K | b |\n" +
		"// a.md:12 (§ A > paragraph) #cccccccccccc  [deleted]\n-gone\n// written\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}
