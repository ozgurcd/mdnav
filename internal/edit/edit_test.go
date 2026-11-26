package edit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozgurcd/mdnav/internal/idpat"
	"github.com/ozgurcd/mdnav/internal/markdown"
	"github.com/ozgurcd/mdnav/internal/nav"
)

const fixture = `# 1. Register

Intro names K2.

| ID | Decision | Source |
|----|----------|--------|
| K1 | Widgets are blue. | Owner |
| K2 | Gadgets use a \| pipe. | Design |
| K3 | Limits apply per lane. |  |

## 1.1 Notes

- first item
  continued
- second item

` + "```" + `
code K1
` + "```" + `
`

func setup(t *testing.T, body string) (string, *idpat.Matcher) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := idpat.New([]idpat.Pattern{{Name: "K", Regex: `K\d+`}})
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

func content(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func str(s string) *string { return &s }

// hashOf returns the hash get reports for target in the file at p.
func hashOf(t *testing.T, p string, m *idpat.Matcher, target string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	tg, err := nav.New([]*markdown.Doc{markdown.Parse(p, b)}, m).Resolve(t.Context(), target, nav.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return tg.Hash
}

func apply(t *testing.T, p string, m *idpat.Matcher, ops ...Op) (Result, error) {
	t.Helper()
	return Apply(t.Context(), m, p, ops, false)
}

func TestSubChangesOnlyTheBlock(t *testing.T) {
	p, m := setup(t, fixture)
	before := hashOf(t, p, m, "K3")
	res, err := apply(t, p, m, Op{Op: OpSub, Target: "K3", Old: str("per lane"), New: "per host", Expect: "#" + before})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(fixture, "per lane", "per host", 1)
	if got := content(t, p); got != want {
		t.Fatalf("file:\n%s\nwant:\n%s", got, want)
	}
	c := res.Changes[0]
	if !res.Written || c.OldStart != 9 || c.NewStart != 9 || c.NewEnd != 9 || c.OldHash != before {
		t.Fatalf("result %+v", res)
	}
	if c.NewHash != hashOf(t, p, m, "K3") || c.NewHash == before {
		t.Fatalf("new hash %s does not match the new block", c.NewHash)
	}
	if c.Removed[0] != "| K3 | Limits apply per lane. |  |" || c.Added[0] != "| K3 | Limits apply per host. |  |" {
		t.Fatalf("diff %q -> %q", c.Removed, c.Added)
	}
}

func TestSubRefusals(t *testing.T) {
	p, m := setup(t, fixture)
	cases := []struct {
		op   Op
		want error
	}{
		{Op{Op: OpSub, Target: "K3", Old: str("per lane"), New: "x", Expect: "000000000000"}, ErrStale},
		{Op{Op: OpSub, Target: "K3", Old: str("not there"), New: "x"}, ErrStale},
		{Op{Op: OpSub, Target: "1.1", Old: str("item"), New: "x"}, ErrInvalid}, // twice in the section
		{Op{Op: OpSub, Target: "K3", New: "x"}, ErrInvalid},
		{Op{Op: OpSub, Target: "K1", Old: str("blue."), New: "blue | red."}, ErrInvalid}, // adds a cell
		{Op{Op: OpSub, Target: "K9", Old: str("x"), New: "y"}, nav.ErrNoTarget},
	}
	for _, c := range cases {
		if _, err := apply(t, p, m, c.op); !errors.Is(err, c.want) {
			t.Errorf("%+v: error %v, want %v", c.op, err, c.want)
		}
	}
	if content(t, p) != fixture {
		t.Fatal("a refused edit changed the file")
	}
}

func TestSetCell(t *testing.T) {
	p, m := setup(t, fixture)
	if _, err := apply(t, p, m, Op{Op: OpSet, Target: "K1", Column: "decision", Value: "Widgets are red | green.", Old: str("Widgets are blue.")}); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(t, p, m, Op{Op: OpSet, Target: "K3", Column: "Source", Value: "Owner"}); err != nil {
		t.Fatal(err)
	}
	got := content(t, p)
	for _, want := range []string{"| K1 | Widgets are red \\| green. | Owner |", "| K3 | Limits apply per lane. | Owner |"} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, c := range []struct {
		op   Op
		want error
	}{
		{Op{Op: OpSet, Target: "K2", Column: "Decision", Value: "x", Old: str("something else")}, ErrStale},
		{Op{Op: OpSet, Target: "K2", Column: "Nope", Value: "x"}, ErrInvalid},
		{Op{Op: OpSet, Target: "1.1", Column: "Decision", Value: "x"}, ErrInvalid},
	} {
		if _, err := apply(t, p, m, c.op); !errors.Is(err, c.want) {
			t.Errorf("%+v: error %v, want %v", c.op, err, c.want)
		}
	}
	// A bare name that resolves to a heading is refused, naming the row form;
	// the row form then works.
	p, m = setup(t, "# Doc\n\n## Alpha\n\n| Name | Value |\n| --- | --- |\n| Alpha | one |\n")
	for target, form := range map[string]string{"Alpha": `"| Alpha |"`, "Doc": `"| <col1> | <col2>"`} {
		_, err := apply(t, p, m, Op{Op: OpSet, Target: target, Column: "Value", Value: "two"})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), form) {
			t.Errorf("set %s: error %v, want ErrInvalid naming %s", target, err, form)
		}
	}
	if _, err := apply(t, p, m, Op{Op: OpSet, Target: "| Alpha |", Column: "Value", Value: "two"}); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceAndDeleteNeedExpectOrForce(t *testing.T) {
	p, m := setup(t, fixture)
	for _, op := range []Op{
		{Op: OpReplace, Target: "K2", Lines: []string{"| K2 | New. | Owner |"}},
		{Op: OpDelete, Target: "K2"},
	} {
		if _, err := apply(t, p, m, op); !errors.Is(err, ErrNeedExpect) {
			t.Errorf("%s without --expect: %v", op.Op, err)
		}
	}
	h := hashOf(t, p, m, "K2")
	if _, err := apply(t, p, m, Op{Op: OpReplace, Target: "K2", Expect: h, Lines: []string{"| K2 | New. | Owner |"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(t, p, m, Op{Op: OpDelete, Target: "K1", Force: true}); err != nil {
		t.Fatal(err)
	}
	got := content(t, p)
	if strings.Contains(got, "| K1 |") || !strings.Contains(got, "| K2 | New. | Owner |\n") {
		t.Fatalf("file:\n%s", got)
	}
}

func TestStructureChecks(t *testing.T) {
	p, m := setup(t, fixture)
	for _, c := range []struct {
		op   Op
		want error
	}{
		{Op{Op: OpReplace, Target: "K2", Force: true, Lines: []string{"just text"}}, ErrInvalid},
		{Op{Op: OpReplace, Target: "Intro", Force: true, Lines: []string{"```", "open"}}, ErrInvalid},
		{Op{Op: OpInsertAfter, Target: "K1", Lines: []string{"a paragraph"}}, ErrInvalid},
		{Op{Op: OpInsertBefore, Target: "doc.md:5", Lines: []string{"| K0 | x | y |"}}, ErrInvalid},
		{Op{Op: OpReplace, Target: "K2", Force: true, Lines: []string{"two\nlines"}}, ErrInvalid},
	} {
		c.op.Target = strings.Replace(c.op.Target, "doc.md", p, 1)
		if _, err := apply(t, p, m, c.op); !errors.Is(err, c.want) {
			t.Errorf("%+v: error %v, want %v", c.op, err, c.want)
		}
	}
	if content(t, p) != fixture {
		t.Fatal("a refused edit changed the file")
	}
}

func TestInsertAndAddRow(t *testing.T) {
	p, m := setup(t, fixture)
	if _, err := apply(t, p, m, Op{Op: OpInsertAfter, Target: "second item", Lines: []string{"- third item"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(t, p, m, Op{Op: OpInsertAfter, Target: "K1", Lines: []string{"| K1a | Between. | Design |"}}); err != nil {
		t.Fatal(err)
	}
	res, err := apply(t, p, m, Op{Op: OpAddRow, Target: "1. Register", Cells: []string{"K4", "Uses a | pipe", "Owner"}})
	if err != nil {
		t.Fatal(err)
	}
	got := content(t, p)
	for _, want := range []string{
		"| K1 | Widgets are blue. | Owner |\n| K1a | Between. | Design |\n| K2 |",
		"|  |\n| K4 | Uses a \\| pipe | Owner |\n\n## 1.1",
		"- second item\n- third item\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if c := res.Changes[0]; c.NewStart != 11 || !strings.Contains(c.NewWhere, "table row K4") {
		t.Errorf("add-row result %+v", c)
	}
	if _, err := apply(t, p, m, Op{Op: OpAddRow, Target: "K2", Cells: []string{"K5", "too few"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("wrong cell count: %v", err)
	}
	if _, err := apply(t, p, m, Op{Op: OpAddRow, Target: "1.1", Cells: []string{"a", "b", "c"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("section without a table: %v", err)
	}
}

func TestDryRunAndAllOrNothing(t *testing.T) {
	p, m := setup(t, fixture)
	res, err := Apply(t.Context(), m, p, []Op{{Op: OpSub, Target: "K3", Old: str("lane"), New: "host"}}, true)
	if err != nil || res.Written || !res.DryRun || len(res.Changes) != 1 || content(t, p) != fixture {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	_, err = apply(t, p, m,
		Op{Op: OpSub, Target: "K3", Old: str("lane"), New: "host"},
		Op{Op: OpSub, Target: "K1", Old: str("absent"), New: "x"},
	)
	if !errors.Is(err, ErrStale) || content(t, p) != fixture {
		t.Fatalf("a failing batch must write nothing: %v", err)
	}
	// Later edits see earlier ones: the second sub finds the first one's text.
	if _, err := apply(t, p, m,
		Op{Op: OpSub, Target: "K3", Old: str("lane"), New: "host"},
		Op{Op: OpSub, Target: "K3", Old: str("per host"), New: "per region"},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content(t, p), "Limits apply per region.") {
		t.Fatal("batch did not apply in order")
	}
}

func TestCRLFAndPathLineTargets(t *testing.T) {
	crlf := strings.ReplaceAll(fixture, "\n", "\r\n")
	p, m := setup(t, crlf)
	if _, err := apply(t, p, m, Op{Op: OpInsertAfter, Target: p + ":3", Lines: []string{"", "Second paragraph."}}); err != nil {
		t.Fatal(err)
	}
	got := content(t, p)
	if !strings.Contains(got, "Intro names K2.\r\n\r\nSecond paragraph.\r\n") || strings.Count(got, "\r\n") != strings.Count(got, "\n") {
		t.Fatalf("CRLF not kept:\n%q", got)
	}
	var amb *nav.AmbiguousError
	if _, err := apply(t, p, m, Op{Op: OpSub, Target: "item", Old: str("item"), New: "x"}); !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("ambiguous target: %v", err)
	}
}

func TestBlankLinesAroundInsertAndDelete(t *testing.T) {
	p, m := setup(t, "# A\n\npara a\n\n# B\n\npara b\n")
	// Insert after a section: before its trailing blank line, not against # B.
	if _, err := apply(t, p, m, Op{Op: OpInsertAfter, Target: p + ":1", Lines: []string{"", "new para"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := content(t, p), "# A\n\npara a\n\nnew para\n\n# B\n\npara b\n"; got != want {
		t.Fatalf("insert after section:\n%q\nwant\n%q", got, want)
	}
	// Delete a paragraph between blank lines: one blank line goes with it.
	if _, err := apply(t, p, m, Op{Op: OpDelete, Target: "new para", Force: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := content(t, p), "# A\n\npara a\n\n# B\n\npara b\n"; got != want {
		t.Fatalf("delete paragraph:\n%q\nwant\n%q", got, want)
	}
	// Deleting the last block keeps the blank line before it: no blank line
	// follows "para b", so there is nothing to collapse.
	if _, err := apply(t, p, m, Op{Op: OpDelete, Target: "para b", Force: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := content(t, p), "# A\n\npara a\n\n# B\n\n"; got != want {
		t.Fatalf("delete last paragraph:\n%q\nwant\n%q", got, want)
	}
}

func TestReplaceSectionKeepsBlankBeforeNextHeading(t *testing.T) {
	p, m := setup(t, "# Notes\n\n## Unreleased\n\n- a\n\n## v1\n\n- b\n")
	if _, err := apply(t, p, m, Op{Op: OpReplace, Target: "Unreleased", Force: true, Lines: []string{"## v2", "", "- a"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := content(t, p), "# Notes\n\n## v2\n\n- a\n\n## v1\n\n- b\n"; got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}
