//go:build !linux

package wslcopy

import "errors"

func startRetirementProcess(string, ...string) error {
	return errors.New("background retirement requires WSL")
}
