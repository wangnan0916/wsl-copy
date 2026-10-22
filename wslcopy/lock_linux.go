package wslcopy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Keep the inode in place: unlinking a lock file would let later callers lock a
// different inode. Go opens it close-on-exec, so retirement cannot inherit it.
func acquireCopyLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("copy lock must be a regular file")
	}
	if err == nil {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrBusy, path)
		}
		return nil, err
	}
	return file, nil
}
