package fileio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateWritesAndKeepsMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	write(t, p, "old\n", 0o640)
	err := Update(t.Context(), p, true, func(raw []byte) ([]byte, error) {
		return bytes.ToUpper(raw), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "OLD\n" {
		t.Fatalf("content %q", got)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %v", entries)
	}
}

func TestDryRunAndErrorsLeaveTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	write(t, p, "keep\n", 0o644)
	if err := Update(t.Context(), p, false, func([]byte) ([]byte, error) { return []byte("x"), nil }); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := Update(t.Context(), p, true, func([]byte) ([]byte, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("error %v", err)
	}
	if got := read(t, p); got != "keep\n" {
		t.Fatalf("content %q", got)
	}
}

func TestRefusesSymlinksAndDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	write(t, target, "x\n", 0o644)
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	called := false
	fn := func(raw []byte) ([]byte, error) { called = true; return raw, nil }
	for _, p := range []string{link, dir} {
		if err := Update(t.Context(), p, true, fn); !errors.Is(err, ErrNotRegular) {
			t.Errorf("%s: error %v, want ErrNotRegular", p, err)
		}
	}
	if called {
		t.Error("fn ran for a refused path")
	}
}

// Concurrent updates must not lose each other's changes: every append lands.
func TestConcurrentUpdatesSerialize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	write(t, p, "", 0o644)
	const workers, each = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := range workers {
		wg.Go(func() {
			for i := range each {
				errs <- Update(context.Background(), p, true, func(raw []byte) ([]byte, error) {
					return append(raw, fmt.Sprintf("w%d-%d\n", w, i)...), nil
				})
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(read(t, p), "\n"); n != workers*each {
		t.Fatalf("%d lines, want %d: an update was lost", n, workers*each)
	}
}

func TestLockWaitHonoursContext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	write(t, p, "x\n", 0o644)
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Update(context.Background(), p, false, func(raw []byte) ([]byte, error) {
			close(held)
			<-release
			return raw, nil
		})
	}()
	<-held
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Update(ctx, p, true, func(raw []byte) ([]byte, error) { return raw, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled while the lock is held", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
