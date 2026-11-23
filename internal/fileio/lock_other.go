//go:build !unix

package fileio

import (
	"errors"
	"os"
)

// tryLock is unavailable here: mdnav edits files only on Unix systems.
func tryLock(*os.File) (bool, error) {
	return false, errors.New("file locking is supported only on Unix systems")
}
