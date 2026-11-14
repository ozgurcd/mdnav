package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rel(t *testing.T, root string, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		r, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestWalkWithoutGit(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{
		"a.md":         "# A\n",
		"sub/b.md":     "# B\n",
		"sub/c.txt":    "not markdown",
		".git/HEAD.md": "ignored",
		"ignored/x.md": "# X\n",
		".gitignore":   "ignored/\n",
	})
	files, err := walk(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	// Without git, .gitignore is not consulted but .git is always skipped.
	if got, want := rel(t, root, files), []string{"a.md", "ignored/x.md", "sub/b.md"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDiscoverInGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	write(t, root, map[string]string{
		"a.md":         "# A\n",
		"sub/b.md":     "# B\n",
		"ignored/x.md": "# X\n",
		".gitignore":   "ignored/\n",
	})
	cmd := exec.CommandContext(t.Context(), "git", "-C", root, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	files, err := Discover(t.Context(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rel(t, root, files), []string{"a.md", "sub/b.md"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// An explicit file is always taken, even when ignored, and duplicates collapse.
	x := filepath.Join(root, "ignored", "x.md")
	files, err = Discover(t.Context(), []string{x, root, x})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rel(t, root, files), []string{"a.md", "ignored/x.md", "sub/b.md"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLoadKeepsOrderAndPaths(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"b.md": "b\n", "a.md": "a\r\nline\r\n", "empty.md": ""})
	docs, err := Parse(t.Context(), []string{filepath.Join(root, "b.md"), filepath.Join(root, "a.md"), filepath.Join(root, "empty.md")})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 || filepath.Base(docs[0].Path) != "b.md" || docs[1].NumLines() != 2 || docs[2].NumLines() != 0 {
		t.Fatalf("docs: %v", docs)
	}
}

func TestMissingPath(t *testing.T) {
	if _, err := Discover(t.Context(), []string{filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("missing path should fail")
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, []string{t.TempDir()}); err == nil {
		t.Fatal("cancelled context should fail")
	}
}
