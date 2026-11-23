// Package fileio changes a file safely: one writer at a time, never a
// half-written file, and never through a symbolic link.
//
// Update takes an exclusive lock on the file, reads it, lets the caller
// compute the new contents, writes them to a temporary file in the same
// directory, syncs it and renames it over the original. Readers see either
// the old file or the new one. Two Update calls on one file, in one process
// or several, run one after the other; the second sees the first one's result.
package fileio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrNotRegular is returned for symbolic links and anything but a regular file.
var ErrNotRegular = errors.New("not a regular file (symbolic links are refused)")

// lockPoll is how often a waiting Update retries a held lock.
const lockPoll = 20 * time.Millisecond

// Update replaces path with fn(contents). When write is false, or fn returns
// the same bytes, nothing is written. An error from fn leaves the file as it
// was. The file keeps its permission bits.
func Update(ctx context.Context, path string, write bool, fn func(raw []byte) ([]byte, error)) error {
	f, mode, err := lockFile(ctx, path)
	if err != nil {
		return err
	}
	defer f.Close() // closing releases the lock, after the rename below

	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	out, err := fn(raw)
	if err != nil {
		return err
	}
	if !write || bytes.Equal(out, raw) {
		return nil
	}
	return writeAtomic(path, out, mode)
}

// lockFile opens path and takes an exclusive lock on it. A writer that renames
// a new file over path while we wait leaves us holding the old inode, so after
// locking we check that path still names the file we locked, and retry if not.
func lockFile(ctx context.Context, path string) (*os.File, fs.FileMode, error) {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, 0, err
		}
		if !info.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("%s: %w", path, ErrNotRegular)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, 0, err
		}
		if err := lockWait(ctx, f); err != nil {
			f.Close()
			return nil, 0, fmt.Errorf("%s: lock: %w", path, err)
		}
		locked, err1 := f.Stat()
		current, err2 := os.Lstat(path)
		if err1 == nil && err2 == nil && os.SameFile(locked, current) && current.Mode().IsRegular() {
			return f, current.Mode().Perm(), nil
		}
		f.Close() // replaced or removed while we waited: start again
		if err2 != nil {
			return nil, 0, err2
		}
	}
}

func lockWait(ctx context.Context, f *os.File) error {
	for {
		ok, err := tryLock(f)
		if err != nil || ok {
			return err
		}
		t := time.NewTimer(lockPoll)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// writeAtomic writes data to a new file next to path and renames it over path.
func writeAtomic(path string, data []byte, mode fs.FileMode) (err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+".mdnav-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name()) // best effort: the original is untouched
		}
	}()
	if _, err = tmp.Write(data); err == nil {
		if err = tmp.Chmod(mode); err == nil {
			err = tmp.Sync()
		}
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir makes the rename durable where the platform supports it.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync() // some file systems refuse directory sync; the rename is already visible
	_ = d.Close()
}
