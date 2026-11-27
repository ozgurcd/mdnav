package hook

import (
	"strconv"
	"strings"
	"testing"
)

// TestUnifiedDiff is the diff table: change shapes and their hunks.
func TestUnifiedDiff(t *testing.T) {
	lines := func(n int) []string {
		var l []string
		for i := 1; i <= n; i++ {
			l = append(l, "l"+strconv.Itoa(i))
		}
		return l
	}
	with := func(base []string, i int, repl ...string) []string {
		out := append([]string{}, base[:i]...)
		out = append(out, repl...)
		return append(out, base[i+1:]...)
	}
	base := lines(20)
	rows := []struct {
		name string
		a, b []string
		want string
	}{
		{"same", base, base, ""},
		{"replace one", base, with(base, 9, "X"),
			"--- a/f\n+++ b/f\n@@ -7,7 +7,7 @@\n l7\n l8\n l9\n-l10\n+X\n l11\n l12\n l13\n"},
		{"insert at start", base[:3], append([]string{"N"}, base[:3]...),
			"--- a/f\n+++ b/f\n@@ -1,3 +1,4 @@\n+N\n l1\n l2\n l3\n"},
		{"two hunks", base, with(with(base, 1, "A"), 18, "B"),
			"--- a/f\n+++ b/f\n@@ -1,5 +1,5 @@\n l1\n-l2\n+A\n l3\n l4\n l5\n@@ -16,5 +16,5 @@\n l16\n l17\n l18\n-l19\n+B\n l20\n"},
		{"near changes join", base, with(with(base, 5, "A"), 10, "B"),
			"--- a/f\n+++ b/f\n@@ -3,12 +3,12 @@\n l3\n l4\n l5\n-l6\n+A\n l7\n l8\n l9\n l10\n-l11\n+B\n l12\n l13\n l14\n"},
		{"delete only", base[:4], with(base[:4], 1),
			"--- a/f\n+++ b/f\n@@ -1,4 +1,3 @@\n l1\n-l2\n l3\n l4\n"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			if got := unifiedDiff("f", r.a, r.b); got != r.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, r.want)
			}
		})
	}
}

// TestApplyEdits is the plugin edit table: anchors found once, missing,
// or found twice.
func TestApplyEdits(t *testing.T) {
	edits := []textEdit{{old: []string{"b"}, new: []string{"b", "b2"}}, {old: []string{"const X = ", "d"}, prefix: true, new: []string{"D"}}}
	rows := []struct {
		name, src, want, err string
	}{
		{"applied", "a\nb\nconst X = 1;\nd\n", "a\nb\nb2\nD\n", ""},
		{"missing anchor", "a\nb\nd\n", "", `"const X = " not found`},
		{"twice", "b\nb\nconst X = 1;\nd\n", "", `"b" found 2 times`},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got, err := applyEdits(r.src, edits)
			if r.err != "" {
				if err == nil || !strings.Contains(err.Error(), r.err) {
					t.Fatalf("err %v, want %q", err, r.err)
				}
				return
			}
			if err != nil || got != r.want {
				t.Fatalf("got %q, %v; want %q", got, err, r.want)
			}
		})
	}
}
