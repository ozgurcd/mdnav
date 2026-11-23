package nav

import (
	"regexp"
	"testing"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

var hashRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

func TestBlockHashDependsOnlyOnText(t *testing.T) {
	a := markdown.Parse("a.md", []byte("x\n| K1 | v |\ny\n"))
	b := markdown.Parse("b.md", []byte("other\nlines\nfirst\n| K1 | v |\n"))
	crlf := markdown.Parse("c.md", []byte("x\r\n| K1 | v |\r\ny\r\n"))
	changed := markdown.Parse("d.md", []byte("x\n| K1 | w |\ny\n"))

	h := BlockHash(a, 2, 2)
	if !hashRe.MatchString(h) {
		t.Fatalf("hash %q is not 12 hex digits", h)
	}
	if got := BlockHash(b, 4, 4); got != h {
		t.Errorf("same text at another line: %s, want %s", got, h)
	}
	if got := BlockHash(crlf, 2, 2); got != h {
		t.Errorf("CRLF file: %s, want %s", got, h)
	}
	if got := BlockHash(changed, 2, 2); got == h {
		t.Error("changed text kept the same hash")
	}
	// Line boundaries count: "ab" on one line differs from "a" and "b".
	one := markdown.Parse("e.md", []byte("ab\n"))
	two := markdown.Parse("f.md", []byte("a\nb\n"))
	if BlockHash(one, 1, 1) == BlockHash(two, 1, 2) {
		t.Error("joining lines must not collide with one longer line")
	}
}

func docByName(t *testing.T, n *Navigator, path string) *markdown.Doc {
	t.Helper()
	for _, d := range n.docs {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("%s not loaded", path)
	return nil
}

func TestGetBlocksCarryTheirHash(t *testing.T) {
	n := newNav(t)
	reg := docByName(t, n, "testdata/01-register.md")
	eng := docByName(t, n, "testdata/05-engine.md")

	r := get(t, n, "D4", Options{})
	if got, want := r.Blocks[0].Hash, BlockHash(reg, 10, 10); got != want {
		t.Errorf("row hash %s, want %s (row line only, not the header row)", got, want)
	}
	// The see-also heading points at its whole section.
	if got, want := r.SeeAlso[0].Hash, BlockHash(eng, 15, 21); got != want {
		t.Errorf("see-also heading hash %s, want section hash %s", got, want)
	}

	// A section replaced by its outline still hashes the whole section.
	full := get(t, n, "5.4", Options{})
	capped := get(t, n, "5.4", Options{MaxLines: 5})
	want := BlockHash(eng, 11, 21)
	if full.Blocks[0].Hash != want || capped.Blocks[0].Hash != want {
		t.Errorf("section hashes %s / %s, want %s", full.Blocks[0].Hash, capped.Blocks[0].Hash, want)
	}

	// A cut block hashes the whole block, not the lines shown.
	cut := get(t, n, "porting", Options{MaxLines: 1})
	if got, want := cut.Blocks[0].Hash, BlockHash(eng, 5, 6); got != want {
		t.Errorf("cut list item hash %s, want %s", got, want)
	}
}

func TestRefsAndStatusCarryTheTargetBlockHash(t *testing.T) {
	n := newNav(t)
	reg := docByName(t, n, "testdata/01-register.md")
	eng := docByName(t, n, "testdata/05-engine.md")

	refs, err := n.Refs(t.Context(), "D2", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range refs.Files {
		for _, ref := range f.Refs {
			if !hashRe.MatchString(ref.Hash) {
				t.Errorf("%s:%d has hash %q", f.Path, ref.Line, ref.Hash)
			}
		}
	}
	// 05-engine.md:8 is a nested list item; its hash is that item's.
	if got, want := refs.Files[1].Refs[0].Hash, BlockHash(eng, 8, 8); got != want {
		t.Errorf("nested item hash %s, want %s", got, want)
	}

	st, err := n.Status(t.Context(), "D1", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := st.Hits[0].Hash, BlockHash(reg, 11, 11); got != want {
		t.Errorf("status hit hash %s, want %s", got, want)
	}
}
