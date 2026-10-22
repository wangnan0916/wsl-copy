package wslcopy

import (
	"os"
	"os/exec"
	"syscall"
)

// The one-shot child owns its lifetime and log. Never inherit a caller's pipes,
// terminal session or context, and reap it if the library host stays alive.
func startRetirementProcess(name string, args ...string) error {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
