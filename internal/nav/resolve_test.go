package nav

import (
	"errors"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	n := newNav(t)
	cases := []struct {
		q          string
		path       string
		start, end int
		kind       string
	}{
		{"D4", "testdata/01-register.md", 10, 10, "table row"},
		{"§5.4", "testdata/05-engine.md", 11, 21, "section"},
		{"testdata/05-engine.md:6", "testdata/05-engine.md", 5, 6, "list item"},
		{"./testdata/05-engine.md:15", "testdata/05-engine.md", 15, 21, "section"}, // heading line: its section
		{"testdata/01-register.md:5", "testdata/01-register.md", 5, 5, "table header"},
	}
	for _, c := range cases {
		got, err := n.Resolve(t.Context(), c.q, Options{})
		if err != nil {
			t.Errorf("%s: %v", c.q, err)
			continue
		}
		if got.Path != c.path || got.Start != c.start || got.End != c.end || got.Kind != c.kind || !hashRe.MatchString(got.Hash) {
			t.Errorf("%s: got %+v", c.q, got)
		}
	}
	row, _ := n.Resolve(t.Context(), "D4", Options{})
	if row.Table < 0 || row.IsHeader || row.HeaderLine != 5 {
		t.Errorf("row table info %+v", row)
	}

	var amb *AmbiguousError
	if _, err := n.Resolve(t.Context(), "sprockets", Options{}); !errors.As(err, &amb) || len(amb.Candidates) < 2 ||
		!strings.Contains(amb.Error(), "testdata/01-register.md:9-9") {
		t.Errorf("ambiguous: %v", err)
	}
	for _, q := range []string{"no such text anywhere", "testdata/01-register.md:2"} {
		if _, err := n.Resolve(t.Context(), q, Options{}); !errors.Is(err, ErrNoTarget) {
			t.Errorf("%s: error %v, want ErrNoTarget", q, err)
		}
	}
	if _, err := n.Resolve(t.Context(), "testdata/01-register.md:999", Options{}); err == nil {
		t.Error("line beyond the file should fail")
	}
}

func TestGetPathLine(t *testing.T) {
	n := newNav(t)
	r := get(t, n, "testdata/05-engine.md:15", Options{})
	if r.Match != MatchLine || len(r.Blocks) != 1 || r.Blocks[0].Start != 15 || r.Blocks[0].End != 21 || r.Blocks[0].Kind != "section" {
		t.Fatalf("get path:line: %+v", r)
	}
	if _, err := n.Get(t.Context(), "testdata/05-engine.md:2", Options{}); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("blank line: %v", err)
	}
}
