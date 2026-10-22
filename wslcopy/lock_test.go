//go:build linux

package wslcopy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyLock(t *testing.T) {
	f := newLocalFixture(t)
	path := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "shared.lock")
	lock, err := acquireCopyLock(path)
	must(t, err)
	defer lock.Close()
	opts := f.options()
	opts.LockFile = path
	for _, target := range []string{f.native, f.native + `\child`, `Q:\portable`, `R:\unrelated`} {
		opts.WindowsTarget = target
		output, err := Copy(t.Context(), opts)
		if !errors.Is(err, ErrBusy) || output.DestinationCreated {
			t.Fatalf("overlapping or distinct target bypassed shared lock: %+v; %v", output, err)
		}
	}
	absent(t, f.target)
	must(t, lock.Close())
	// Failure paths release the same persistent inode, without removing the file.
	opts.WindowsTarget = f.native
	t.Setenv("TEST_EXTRACT_FAIL", "1")
	if _, err := Copy(t.Context(), opts); err == nil {
		t.Fatal("expected injected extraction failure")
	}
	lock, err = acquireCopyLock(path)
	must(t, err)
	must(t, lock.Close())

	for _, invalid := range []string{"relative.lock", filepath.Join(f.source, "lock"), filepath.Join(f.parent, "lock")} {
		opts.LockFile = invalid
		if _, err := Copy(t.Context(), opts); err == nil {
			t.Fatal("unsafe lock path accepted:", invalid)
		}
	}
	absent(t, filepath.Join(f.source, "lock"))
	absent(t, filepath.Join(f.parent, "lock"))
	opts.LockFile = filepath.Join(os.Getenv("XDG_CACHE_HOME"), "never-created", "lock")
	opts.DryRun = true
	// Use an absent target after the deliberately retained extraction failure.
	must(t, os.RemoveAll(f.target))
	_, err = Copy(t.Context(), opts)
	must(t, err)
	absent(t, filepath.Dir(opts.LockFile))
}

func TestLockFileTypes(t *testing.T) {
	for _, kind := range []string{"link", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock")
			switch kind {
			case "link":
				must(t, os.Symlink("missing", path))
			case "fifo":
				must(t, syscall.Mkfifo(path, 0600))
			case "directory":
				must(t, os.Mkdir(path, 0700))
			}
			if file, err := acquireCopyLock(path); err == nil {
				file.Close()
				t.Fatal("nonregular lock accepted")
			}
		})
	}
}

func TestLockProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "WSL_COPY_TEST_LOCK="+path)
	stdin, err := cmd.StdinPipe()
	must(t, err)
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	must(t, err)
	must(t, cmd.Start())
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	must(t, err)
	if line != "locked\n" {
		t.Fatal("child did not acquire lock:", line)
	}
	if file, err := acquireCopyLock(path); !errors.Is(err, ErrBusy) {
		if file != nil {
			file.Close()
		}
		t.Fatal("cross-process exclusion failed:", err)
	}
	must(t, cmd.Process.Kill())
	_ = cmd.Wait()
	file, err := acquireCopyLock(path)
	must(t, err)
	must(t, file.Close())
}

func TestLockHelperProcess(t *testing.T) {
	path := os.Getenv("WSL_COPY_TEST_LOCK")
	if path == "" {
		return
	}
	file, err := acquireCopyLock(path)
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
	file.Close()
	os.Exit(0)
}
