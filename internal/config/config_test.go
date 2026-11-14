package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	c, err := Parse([]byte(`{"extra_patterns":[{"name":"ADR","regex":"ADR-\\d+"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if last := c.Patterns[len(c.Patterns)-1]; last.Name != "ADR" {
		t.Fatalf("extra pattern not appended: %+v", last)
	}
	c, err = Parse([]byte(`{"patterns":[{"name":"K","regex":"K\\d+"}]}`))
	if err != nil || len(c.Patterns) != 1 || c.Patterns[0].Name != "K" {
		t.Fatalf("replace: %+v %v", c.Patterns, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, src := range []string{
		`{"pattern":[]}`, // unknown top-level field
		`{"patterns":[{"name":"K","regex":"K","x":1}]}`, // unknown nested field
		`{"patterns":[]}`,                         // empty replacement
		`{"patterns":[{"name":"K","regex":"("}]}`, // bad regex
		`not json`,
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%s) should fail", src)
		}
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Discover(sub)
	if err != nil || c.Source != "" {
		t.Fatalf("without file: %+v %v", c, err)
	}
	p := filepath.Join(root, "a", FileName)
	if err := os.WriteFile(p, []byte(`{"patterns":[{"name":"K","regex":"K\\d+"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = Discover(sub)
	if err != nil || c.Source != p || c.Patterns[0].Name != "K" {
		t.Fatalf("with file: %+v %v", c, err)
	}
}
