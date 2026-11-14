package main

import (
	"bytes"
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
