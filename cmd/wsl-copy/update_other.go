//go:build !linux

package main

import (
	"fmt"
	"io"
)

func runUpdate(_ []string, _ io.Writer, stderr io.Writer) int {
	fmt.Fprintln(stderr, "wsl-copy update: run inside WSL as a Linux executable")
	return 1
}
