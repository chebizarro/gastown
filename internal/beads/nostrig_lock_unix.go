//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package beads

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func acquireNostrigFileLock(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() error {
		unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		closeErr := f.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}
