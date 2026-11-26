package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseInterspersedFlags(t *testing.T) {
	req, timeout, err := parse([]string{"get", "D1", "docs", "--limit", "3", "--json", "more", "--timeout=5s"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Arg != "D1" || !slices.Equal(req.Paths, []string{"docs", "more"}) || req.Options.Limit != 3 || !req.JSON || timeout != 5*time.Second {
		t.Fatalf("got %+v timeout %v", req, timeout)
	}
}

func TestParseDoubleDash(t *testing.T) {
	req, _, err := parse([]string{"get", "--", "--not-a-flag", "docs"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Arg != "--not-a-flag" || !slices.Equal(req.Paths, []string{"docs"}) {
		t.Fatalf("got %+v", req)
	}
	// Text that starts with "-": not flag-shaped needs nothing, flag-shaped
	// needs "--", and a flag's value may start with "-".
	for _, c := range []struct {
		args          []string
		old, new, exp string
	}{
		{[]string{"sub", "D4", "->", "-> done", "--dry-run"}, "->", "-> done", ""},
		{[]string{"sub", "--expect", "-ab", "--", "D4", "-x", "-y"}, "-x", "-y", "-ab"},
	} {
		req, _, err := parse(c.args)
		if err != nil || *req.Edit.Old != c.old || req.Edit.New != c.new || req.Edit.Expect != c.exp {
			t.Errorf("parse(%q) = %+v, %v", c.args, req.Edit, err)
		}
	}
	if _, _, err := parse([]string{"sub", "D4", "-x", "-y"}); err == nil || !strings.Contains(err.Error(), "after --") {
		t.Errorf("flag-shaped text: error %v should point to --", err)
	}
}

func TestParseLimitZeroMeansAll(t *testing.T) {
	req, _, err := parse([]string{"refs", "D1", "--limit", "0"})
	if err != nil || req.Options.Limit != -1 {
		t.Fatalf("got %+v %v", req.Options, err)
	}
}

func TestParseErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"bogus"},
		{"get"},
		{"get", "x", "--nope"},
		{"outline", "a.md", "b.md"},
		{"ids", "--depth", "2"},
		{"get", "x", "--timeout", "0s"},
	} {
		if _, _, err := parse(args); err == nil {
			t.Errorf("parse(%q) should fail", args)
		}
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 0 || out.String() != "mdnav "+version+"\n" {
			t.Fatalf("%q: %d %q %q", args, code, out.String(), errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"version", "--json"}, &out, &errb); code != 0 || out.String() != "{\n  \"version\": \""+version+"\"\n}\n" {
		t.Fatalf("json: %d %q", code, out.String())
	}
	if _, _, err := parse([]string{"version", "extra"}); err == nil {
		t.Fatal("version with an argument should fail")
	}
}

func TestRunExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"get", "Supersedes", "../../internal/nav/testdata"}, &out, &errb); code != 0 {
		t.Fatalf("get: %d %s", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "// ../../internal/nav/testdata/01-register.md:11 ") {
		t.Fatalf("get output: %q", out.String())
	}
	if code := run([]string{"get", "no such text here", "../../internal/nav/testdata"}, &out, &errb); code != 1 {
		t.Fatalf("no match should exit 1, got %d", code)
	}
	if code := run([]string{"get"}, &out, &errb); code != 2 {
		t.Fatalf("usage error should exit 2, got %d", code)
	}
}

func TestParseEditCommands(t *testing.T) {
	req, _, err := parse([]string{"sub", "D4", "per lane", "per host", "docs", "--expect", "#abc", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if e := req.Edit; e.Op != "sub" || e.Target != "D4" || *e.Old != "per lane" || e.New != "per host" || e.Expect != "#abc" ||
		!req.DryRun || !slices.Equal(req.Paths, []string{"docs"}) {
		t.Fatalf("sub: %+v", req)
	}
	req, _, err = parse([]string{"set", "D4", "Owner", "--col", "Source", "--old", ""})
	if err != nil || req.Edit.Op != "set" || req.Edit.Column != "Source" || req.Edit.Value != "Owner" || req.Edit.Old == nil || *req.Edit.Old != "" {
		t.Fatalf("set: %+v %v", req.Edit, err)
	}
	req, _, err = parse([]string{"insert", "--after", "§5.4", "--from", "-", "docs"})
	if err != nil || req.Edit.Op != "insert-after" || req.Edit.Target != "§5.4" || req.From != "-" {
		t.Fatalf("insert: %+v %v", req, err)
	}
	req, _, err = parse([]string{"add-row", "D5", "--cells", `D6|a \| b|Owner`})
	if err != nil || !slices.Equal(req.Edit.Cells, []string{"D6", "a | b", "Owner"}) {
		t.Fatalf("add-row: %+v %v", req.Edit, err)
	}
	for _, args := range [][]string{
		{"sub", "D4", "only-old"},
		{"set", "D4", "v"},
		{"replace", "D4"},
		{"insert", "--before", "a", "--after", "b", "--from", "f"},
		{"insert", "--from", "f"},
		{"add-row", "D4"},
		{"delete", "D4", "--force", "--expect", "abc"},
		{"apply"},
		{"apply", "a.json", "b.json"},
	} {
		if _, _, err := parse(args); err == nil {
			t.Errorf("parse(%q) should fail", args)
		}
	}
}

func TestRunEditExitCodes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.md")
	if err := os.WriteFile(p, []byte("# A\n\n| ID | Text |\n|----|------|\n| D1 | old text |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"sub", "D1", "old", "new", p}, &out, &errb); code != 0 {
		t.Fatalf("sub: %d %s", code, errb.String())
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "| D1 | new text |") {
		t.Fatalf("file: %s", b)
	}
	if !strings.Contains(out.String(), "+| D1 | new text |") || !strings.Contains(out.String(), "// written") {
		t.Fatalf("output: %s", out.String())
	}
	if code := run([]string{"sub", "D1", "new", "x", p, "--expect", "000000000000"}, &out, &errb); code != 3 {
		t.Fatalf("stale --expect should exit 3, got %d", code)
	}
	if code := run([]string{"delete", "D9", p, "--force"}, &out, &errb); code != 1 {
		t.Fatalf("missing target should exit 1, got %d", code)
	}
	if code := run([]string{"delete", "D1", p}, &out, &errb); code != 2 {
		t.Fatalf("delete without --expect/--force should exit 2, got %d", code)
	}
}
