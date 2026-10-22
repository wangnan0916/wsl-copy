//go:build !linux

package wslcopy

import (
	"errors"
	"os"
)

func acquireCopyLock(string) (*os.File, error) {
	return nil, errors.New("copy locking requires WSL")
}

func startRetirementProcess(string, ...string) error {
	return errors.New("background retirement requires WSL")
}
