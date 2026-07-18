//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package beads

func acquireNostrigFileLock(path string) (func() error, error) {
	return func() error { return nil }, nil
}
