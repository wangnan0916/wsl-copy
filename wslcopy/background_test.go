//go:build linux

package wslcopy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Only tests wait for retirement, so fixture teardown cannot race a detached child.
func waitRetirements(parent string) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		containers, err := filepath.Glob(filepath.Join(parent, ".wsl-copy-*"))
		if err != nil {
			return err
		}
		pending := false
		for _, container := range containers {
			log, err := os.ReadFile(filepath.Join(container, "cleanup.log"))
			if errors.Is(err, os.ErrNotExist) {
				_, containerErr := os.Lstat(container)
				_, previousErr := os.Lstat(filepath.Join(container, "previous"))
				_, partialErr := os.Lstat(filepath.Join(container, "partial"))
				// A failure without a worker keeps previous/partial, but an empty
				// container may be between log removal and final deletion/recreation.
				pending = pending || (containerErr == nil && previousErr != nil && partialErr != nil)
				continue
			}
			if errors.Is(err, os.ErrPermission) {
				// Windows log creation/writes can briefly deny a concurrent test read.
				pending = true
				continue
			}
			if err != nil {
				return err
			}
			pending = pending || !strings.Contains(string(log), "failed:")
		}
		if !pending {
			return nil
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("retirement did not finish in fixture %s", parent)
		case <-tick.C:
		}
	}
}

func TestBackgroundRetirement(t *testing.T) {
	f := newLocalFixture(t)
	must(t, os.Mkdir(f.target, 0700))
	writeFile(t, filepath.Join(f.target, "old-only"), "previous")
	gatePath, readyPath := filepath.Join(t.TempDir(), "gate"), filepath.Join(t.TempDir(), "ready")
	for _, path := range []string{gatePath, readyPath} {
		must(t, syscall.Mkfifo(path, 0600))
	}
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0)
	must(t, err)
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("release\n") }()
	ready, err := os.OpenFile(readyPath, os.O_RDWR, 0)
	must(t, err)
	defer ready.Close()
	t.Setenv("TEST_CLEANUP_GATE", gatePath)
	t.Setenv("TEST_CLEANUP_READY", readyPath)
	opts := f.options()
	opts.Replace = true
	opts.LockFile = filepath.Join(os.Getenv("XDG_CACHE_HOME"), "copy.lock")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		output, err := Copy(ctx, opts)
		done <- outcome{output, err}
	}()
	select {
	case output := <-done:
		must(t, output.err)
		if output.result.CleanupError != "" || output.result.PreviousWindowsPath != "" {
			t.Fatal("normal result exposed retirement state:", output.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copy waited for blocked retirement")
	}
	// The detached child must outlive both Copy and its caller's context.
	cancel()
	timer := time.AfterFunc(5*time.Second, func() { ready.Close() })
	defer timer.Stop()
	line, err := bufio.NewReader(ready).ReadString('\n')
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	must(t, err)
	group, err := syscall.Getpgid(pid)
	must(t, err)
	if group != pid {
		t.Fatal("cleanup inherited the caller's process group")
	}
	assertTree(t, f.source, f.target)
	containers, err := filepath.Glob(filepath.Join(f.parent, ".wsl-copy-*"))
	must(t, err)
	if len(containers) != 1 || readFile(t, filepath.Join(containers[0], "previous", "old-only")) != "previous" {
		t.Fatal("old data was not retained while cleanup was blocked")
	}
	// A second copy can finish before the first retirement; each child owns only its container.
	t.Setenv("TEST_CLEANUP_GATE", "")
	writeFile(t, filepath.Join(f.source, "notes.txt"), "second copy")
	output, err := Copy(t.Context(), opts)
	must(t, err)
	if output.CleanupError != "" {
		t.Fatal(output.CleanupError)
	}
	_, err = gate.WriteString("release\n")
	must(t, err)
	must(t, waitRetirements(f.parent))
	assertTree(t, f.source, f.target)
	containers, err = filepath.Glob(filepath.Join(f.parent, ".wsl-copy-*"))
	must(t, err)
	if len(containers) != 0 {
		t.Fatal("background cleanup left old containers")
	}
}
