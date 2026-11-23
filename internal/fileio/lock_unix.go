//go:build unix

package fileio

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive flock on f without blocking. It reports false
// when another open file description holds the lock.
func tryLock(f *os.File) (bool, error) {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, syscall.EWOULDBLOCK):
			return false, nil
		case errors.Is(err, syscall.EINTR):
			continue
		default:
			return false, err
		}
	}
}
