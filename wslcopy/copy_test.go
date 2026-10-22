//go:build linux

package wslcopy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCopy(t *testing.T) {
	t.Run("silent-by-default", func(t *testing.T) {
		f := newLocalFixture(t)
		writeScript(t, filepath.Join(f.bin, "powershell.exe"), "echo 'native stdout'\necho 'native stderr' >&2\n")
		capture, err := os.CreateTemp(t.TempDir(), "stdio-")
		must(t, err)
		defer capture.Close()
		var output Result
		func() {
			stdout, stderr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = capture, capture
			defer func() { os.Stdout, os.Stderr = stdout, stderr }()
			output, err = Copy(t.Context(), f.options())
		}()
		must(t, err)
		if !output.DestinationCreated || readFile(t, capture.Name()) != "" {
			t.Fatal("library wrote to process stdout/stderr")
		}
	})
	t.Run("cancel-during-extraction", func(t *testing.T) {
		f := newLocalFixture(t)
		readyPath := filepath.Join(t.TempDir(), "ready")
		must(t, syscall.Mkfifo(readyPath, 0600))
		ready, err := os.OpenFile(readyPath, os.O_RDWR, 0)
		must(t, err)
		defer ready.Close()
		t.Setenv("TEST_READY", readyPath)
		writeScript(t, filepath.Join(f.bin, "tar.exe"), "printf 'partial data' > \"$TEST_TARGET/partial\"\nprintf 1 > \"$TEST_READY\"\nexec sleep 30\n")
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		stop := context.AfterFunc(ctx, func() { ready.Close() })
		defer stop()
		type outcome struct {
			result Result
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			output, err := Copy(ctx, f.options())
			done <- outcome{output, err}
		}()
		// Block on a pipe, not polling: the extractor must have started and written partial data.
		var marker [1]byte
		_, readyErr := io.ReadFull(ready, marker[:])
		cancel()
		output := <-done
		must(t, readyErr)
		if !errors.Is(output.err, context.Canceled) || !output.result.DestinationCreated {
			t.Fatalf("cancellation identity or ownership lost: %+v; %v", output.result, output.err)
		}
		if readFile(t, filepath.Join(f.target, "partial")) != "partial data" {
			t.Fatal("partial destination removed after cancellation")
		}
		entries, err := os.ReadDir(f.archives)
		must(t, err)
		if len(entries) != 0 {
			t.Fatal("temporary archive leaked after cancellation")
		}
	})
	t.Run("canceled-context", func(t *testing.T) {
		f := newLocalFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		output, err := Copy(ctx, f.options())
		if !errors.Is(err, context.Canceled) || output.DestinationCreated {
			t.Fatalf("caller cancellation not respected: %+v; %v", output, err)
		}
		absent(t, f.target)
	})
	for _, kind := range []string{"file", "empty", "nonempty", "dangling"} {
		t.Run("existing-"+kind, func(t *testing.T) {
			f := newLocalFixture(t)
			switch kind {
			case "file":
				writeFile(t, f.target, "keep")
			case "dangling":
				must(t, os.Symlink(filepath.Join(f.parent, "missing"), f.target))
			default:
				must(t, os.Mkdir(f.target, 0700))
				if kind == "nonempty" {
					writeFile(t, filepath.Join(f.target, "sentinel"), "keep")
				}
			}
			for _, dryRun := range []bool{false, true} {
				opts := f.options()
				opts.DryRun = dryRun
				output, err := Copy(t.Context(), opts)
				if err == nil || output.DestinationCreated {
					t.Fatalf("existing destination not refused (dry-run=%t): %+v; %v", dryRun, output, err)
				}
			}
			if kind == "file" && readFile(t, f.target) != "keep" || kind == "nonempty" && readFile(t, filepath.Join(f.target, "sentinel")) != "keep" {
				t.Fatal("existing content modified")
			}
			if kind == "dangling" {
				if _, err := os.Readlink(f.target); err != nil {
					t.Fatal("existing link modified:", err)
				}
			} else if kind != "file" {
				absent(t, filepath.Join(f.target, "notes.txt"))
			}
		})
	}
	t.Run("dry-run", func(t *testing.T) {
		f := newLocalFixture(t)
		var diagnostics bytes.Buffer
		opts := f.options()
		opts.DryRun, opts.Diagnostics = true, &diagnostics
		output, err := Copy(t.Context(), opts)
		if err != nil || output.DestinationCreated || output.SourcePath != f.source || output.WindowsPath != f.native {
			t.Fatalf("dry run failed: %+v; %v", output, err)
		}
		if !strings.Contains(diagnostics.String(), filepath.Join(f.bin, "powershell.exe")) ||
			strings.Contains(diagnostics.String(), `$ "tar" `) || strings.Contains(diagnostics.String(), filepath.Join(f.bin, "tar.exe")) {
			t.Fatal("dry run skipped native validation or ran tar:", diagnostics.String())
		}
		absent(t, f.target)
	})
	for _, name := range []string{"tar.exe", "powershell.exe", "tar", "wslpath"} {
		t.Run("missing-"+name, func(t *testing.T) {
			f := newLocalFixture(t)
			writeScript(t, filepath.Join(f.bin, "tar"), "exec \"$TEST_GNU_TAR\" \"$@\"\n")
			must(t, os.Remove(filepath.Join(f.bin, name)))
			t.Setenv("PATH", f.bin)
			for _, dryRun := range []bool{false, true} {
				var diagnostics bytes.Buffer
				opts := f.options()
				opts.DryRun, opts.Diagnostics = dryRun, &diagnostics
				output, err := Copy(t.Context(), opts)
				if err == nil || output.DestinationCreated || !strings.Contains(err.Error(), "required executable not found: "+name) {
					t.Fatalf("missing tool not rejected during preflight: %+v; %v", output, err)
				}
				if diagnostics.Len() != 0 {
					t.Fatal("subprocess ran before tool preflight completed:", diagnostics.String())
				}
				absent(t, f.target)
			}
		})
	}
	for _, failure := range []string{"tool", "native-check", "windows-source", "mapping", "missing-parent", "file-parent", "symlink-parent", "invalid-source"} {
		t.Run(failure, func(t *testing.T) {
			f := newLocalFixture(t)
			var diagnostics bytes.Buffer
			opts := f.options()
			opts.Diagnostics = &diagnostics
			switch failure {
			case "tool":
				opts.WindowsTar = filepath.Join(f.bin, "missing")
			case "native-check":
				t.Setenv("TEST_NATIVE_ERROR", "1")
			case "windows-source":
				t.Setenv("TEST_SOURCE_NATIVE", `R:\source`)
			case "mapping":
				t.Setenv("TEST_PARENT_NATIVE", `R:\wrong-volume`)
			case "missing-parent", "file-parent", "symlink-parent":
				must(t, os.Remove(f.parent))
				if failure == "symlink-parent" {
					must(t, os.Symlink(f.source, f.parent))
				} else if failure == "file-parent" {
					writeFile(t, f.parent, "keep parent file")
				}
			case "invalid-source":
				writeFile(t, filepath.Join(f.source, "CONOUT$"), "invalid")
			}
			for _, dryRun := range []bool{false, true} {
				opts.DryRun = dryRun
				diagnostics.Reset()
				output, err := Copy(t.Context(), opts)
				if err == nil || output.DestinationCreated {
					t.Fatalf("failure not handled (dry-run=%t): %+v; %v", dryRun, output, err)
				}
				if failure == "native-check" && (!strings.Contains(err.Error(), "preparation refused") || strings.Contains(diagnostics.String(), `"wslpath" "-u"`)) {
					t.Fatal("parent conversion ran before native validation:", diagnostics.String(), err)
				}
			}
			if failure == "file-parent" {
				if readFile(t, f.parent) != "keep parent file" {
					t.Fatal("parent file modified")
				}
			} else {
				absent(t, f.target)
			}
		})
	}
}

func TestCreateAndCopyRejectsLateDestination(t *testing.T) {
	f := newLocalFixture(t)
	absent(t, f.target)
	// Reproduce the state at the final write boundary, without a scheduler-dependent race.
	must(t, os.Mkdir(f.target, 0700))
	writeFile(t, filepath.Join(f.target, "sentinel"), "other caller")
	var diagnostics bytes.Buffer
	runner := commandRunner{t.Context(), &diagnostics}
	created, err := runner.createAndCopy(f.source, f.target, f.native, filepath.Join(f.bin, "tar.exe"))
	if !errors.Is(err, os.ErrExist) || created {
		t.Fatalf("late destination was accepted or claimed: created=%t; %v", created, err)
	}
	if diagnostics.Len() != 0 {
		t.Fatal("archive transfer started despite losing exclusive creation:", diagnostics.String())
	}
	if readFile(t, filepath.Join(f.target, "sentinel")) != "other caller" {
		t.Fatal("competing destination modified")
	}
	absent(t, filepath.Join(f.target, "notes.txt"))
}
