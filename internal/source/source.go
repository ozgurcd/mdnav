// Package source finds and parses the Markdown files to navigate. It only
// reads files; it never writes them.
package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/ozgurcd/mdnav/internal/markdown"
)

// Discover expands paths into a sorted, de-duplicated list of files. A file
// argument is taken as given. A directory is searched recursively for *.md
// files, skipping .git and, inside a git work tree, git-ignored paths.
func Discover(ctx context.Context, paths []string) ([]string, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(p)
			continue
		}
		files, err := listDir(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			add(f)
		}
	}
	slices.Sort(out)
	return out, nil
}

func listDir(ctx context.Context, dir string) ([]string, error) {
	files, err := gitList(ctx, dir)
	if err == nil {
		return files, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	// Not a git work tree, or git is unavailable: walk the directory.
	return walk(ctx, dir)
}

// gitList asks git for tracked and untracked, non-ignored *.md files under dir.
func gitList(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard", "--", "*.md")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var out []string
	for rel := range strings.SplitSeq(string(stdout), "\x00") {
		if rel == "" {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		// --cached lists tracked files that were deleted from the work tree.
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out, nil
}

func walk(ctx context.Context, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			if e.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type().IsRegular() && strings.EqualFold(filepath.Ext(p), ".md") {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// Load discovers and parses the files under paths, in sorted order.
func Load(ctx context.Context, paths []string) ([]*markdown.Doc, error) {
	files, err := Discover(ctx, paths)
	if err != nil {
		return nil, err
	}
	return Parse(ctx, files)
}

// Parse reads and parses files concurrently. The result keeps the input order.
func Parse(ctx context.Context, files []string) ([]*markdown.Doc, error) {
	docs := make([]*markdown.Doc, len(files))
	errs := make([]error, len(files))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), max(len(files), 1)) {
		wg.Go(func() {
			for i := range work {
				if err := ctx.Err(); err != nil {
					errs[i] = err
					continue
				}
				data, err := os.ReadFile(files[i])
				if err != nil {
					errs[i] = err
					continue
				}
				docs[i] = markdown.Parse(filepath.ToSlash(files[i]), data)
			}
		})
	}
	for i := range files {
		work <- i
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return docs, errors.Join(errs...)
}
