//go:build linux

package wslcopy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCopyTransfer(t *testing.T) {
	for _, mode := range []string{"content", "empty", "pack-failure", "extract-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newLocalFixture(t)
			// The library must not read the CLI-specific environment defaults.
			for _, name := range []string{"WSL_COPY_WINDOWS_TAR", "WSL_COPY_POWERSHELL"} {
				t.Setenv(name, filepath.Join(f.bin, "missing"))
			}
			switch mode {
			case "content":
				writeFile(t, filepath.Join(f.source, "binary"), "\x00\xffpayload\r\n")
				must(t, os.Link(filepath.Join(f.source, "binary"), filepath.Join(f.source, "hardlink")))
				writeFile(t, filepath.Join(f.source, ".hidden"), "included")
				must(t, os.MkdirAll(filepath.Join(f.source, "中文 O'Brien", "empty"), 0700))
				old := time.Unix(946684800, 0)
				must(t, os.Chtimes(filepath.Join(f.source, "binary"), old, old))
				t.Setenv("TAR_OPTIONS", "--exclude=notes.txt --ignore-failed-read")
			case "empty":
				must(t, os.Remove(filepath.Join(f.source, "notes.txt")))
			case "pack-failure":
				writeScript(t, filepath.Join(f.bin, "tar"), "echo 'simulated packing failure' >&2\nexit 2\n")
			case "extract-failure":
				t.Setenv("TEST_EXTRACT_FAIL", "1")
			}
			var diagnostics bytes.Buffer
			opts := f.options()
			opts.Diagnostics = &diagnostics
			output, err := Copy(t.Context(), opts)
			if !output.DestinationCreated || output.SourcePath != f.source || output.WindowsPath != f.native {
				t.Fatalf("incorrect result or ownership: %+v; %v", output, err)
			}
			if strings.HasSuffix(mode, "failure") {
				want := "tar.exe failed (2): simulated extraction failure"
				if mode == "pack-failure" {
					want = "tar failed (2): simulated packing failure"
					entries, err := os.ReadDir(f.target)
					must(t, err)
					if len(entries) != 0 {
						t.Fatal("packing failure did not retain an empty destination")
					}
					if strings.Contains(diagnostics.String(), filepath.Join(f.bin, "tar.exe")) {
						t.Fatal("extraction started after packing failed:", diagnostics.String())
					}
				}
				if err == nil || err.Error() != want {
					t.Fatalf("wrong failure stage: got %v, want %q", err, want)
				}
				if mode == "extract-failure" && readFile(t, filepath.Join(f.target, "partial")) != "partial data" {
					t.Fatal("partial content removed")
				}
			} else {
				must(t, err)
				assertTree(t, f.source, f.target)
				if mode == "content" {
					binary, err := os.Stat(filepath.Join(f.target, "binary"))
					must(t, err)
					hardlink, err := os.Stat(filepath.Join(f.target, "hardlink"))
					must(t, err)
					if os.SameFile(binary, hardlink) || binary.ModTime().Unix() == 946684800 {
						t.Fatal("hard links or source timestamps were preserved")
					}
				}
			}
			entries, err := os.ReadDir(f.archives)
			must(t, err)
			if len(entries) != 0 {
				t.Fatal("temporary archive leaked")
			}
		})
	}
}
