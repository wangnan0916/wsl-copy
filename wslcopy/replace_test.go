//go:build linux

package wslcopy

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplace(t *testing.T) {
	for _, mode := range []string{"absent", "replace-twice", "empty", "dry-run", "invalid-source", "pack-failure", "extract-failure", "cleanup-failure", "preserve-collision"} {
		t.Run(mode, func(t *testing.T) {
			f := newLocalFixture(t)
			if mode != "absent" {
				must(t, os.Mkdir(f.target, 0700))
				if mode != "empty" {
					writeFile(t, filepath.Join(f.target, "old-only"), "keep previous")
				}
			}
			opts := f.options()
			opts.Replace = true
			switch mode {
			case "dry-run":
				opts.DryRun = true
			case "invalid-source":
				writeFile(t, filepath.Join(f.source, "CONOUT$"), "invalid")
			case "pack-failure":
				writeScript(t, filepath.Join(f.bin, "tar"), `
for previous in "$TEST_PARENT"/.wsl-copy-*/previous; do test -f "$previous/old-only"; done
test ! -e "$TEST_TARGET/old-only"
echo 'packing failed after retirement' >&2
exit 23
`)
			case "extract-failure":
				t.Setenv("TEST_EXTRACT_FAIL", "1")
			case "cleanup-failure":
				t.Setenv("TEST_CLEANUP_FAIL", "1")
			case "preserve-collision":
				writeScript(t, filepath.Join(f.bin, "tar.exe"), `
for container in "$TEST_PARENT"/.wsl-copy-*; do
  mkdir "$container/partial"
  printf 'foreign content' > "$container/partial/sentinel"
done
printf 'partial content' > "$TEST_TARGET/partial"
echo 'extract failed' >&2
exit 23
`)
			}
			output, err := Copy(t.Context(), opts)
			containers, globErr := filepath.Glob(filepath.Join(f.parent, ".wsl-copy-*"))
			must(t, globErr)
			switch mode {
			case "dry-run", "invalid-source":
				if (err != nil) != (mode == "invalid-source") || output.DestinationCreated || len(containers) != 0 {
					t.Fatalf("preflight mutated target: %+v; %v", output, err)
				}
				if readFile(t, filepath.Join(f.target, "old-only")) != "keep previous" {
					t.Fatal("old content changed")
				}
			case "pack-failure", "extract-failure":
				if err == nil || !output.DestinationCreated || !output.DestinationRestored || output.PreviousWindowsPath != "" || len(containers) != 1 {
					t.Fatalf("failed copy not rolled back: %+v; %v", output, err)
				}
				wantError := "tar.exe failed (2): simulated extraction failure"
				if mode == "pack-failure" {
					wantError = "tar failed (23): packing failed after retirement"
				}
				if err.Error() != wantError || readFile(t, filepath.Join(f.target, "old-only")) != "keep previous" {
					t.Fatal("wrong failure stage or previous data lost:", err)
				}
				wantPath := `Q:\portable\` + filepath.Base(containers[0]) + `\partial`
				if output.RetainedWindowsPath != wantPath {
					t.Fatalf("wrong retained path: %+v", output)
				}
				if mode == "extract-failure" && readFile(t, filepath.Join(containers[0], "partial", "partial")) != "partial data" {
					t.Fatal("failed output lost")
				}
				absent(t, filepath.Join(containers[0], "previous"))
			case "preserve-collision":
				if err == nil || !errors.Is(err, os.ErrExist) || output.DestinationRestored || output.PreviousWindowsPath == "" || output.RetainedWindowsPath != f.native || len(containers) != 1 {
					t.Fatalf("preservation collision not handled: %+v; %v", output, err)
				}
				if readFile(t, filepath.Join(containers[0], "partial", "sentinel")) != "foreign content" || readFile(t, filepath.Join(containers[0], "previous", "old-only")) != "keep previous" {
					t.Fatal("foreign or previous content changed")
				}
			default:
				must(t, err)
				assertTree(t, f.source, f.target)
				if !output.DestinationCreated || output.DestinationRestored {
					t.Fatalf("incorrect replacement result: %+v", output)
				}
				if mode == "cleanup-failure" {
					if len(containers) != 1 || output.PreviousWindowsPath == "" || !strings.Contains(output.CleanupError, "simulated cleanup failure") {
						t.Fatalf("cleanup failure lost: %+v", output)
					}
					if readFile(t, filepath.Join(containers[0], "previous", "old-only")) != "keep previous" {
						t.Fatal("cleanup failure lost old data")
					}
				} else if len(containers) != 0 || output.PreviousWindowsPath != "" || output.CleanupError != "" {
					t.Fatalf("successful retirement left residue: %+v", output)
				}
				if mode == "replace-twice" {
					writeFile(t, filepath.Join(f.source, "notes.txt"), "second copy")
					output, err = Copy(t.Context(), opts)
					must(t, err)
					assertTree(t, f.source, f.target)
					if output.CleanupError != "" {
						t.Fatal(output.CleanupError)
					}
				}
			}
		})
	}
}

func TestMoveDirectoryRefusesExisting(t *testing.T) {
	for _, kind := range []string{"directory", "file", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			f := newLocalFixture(t)
			switch kind {
			case "directory":
				must(t, os.Mkdir(f.target, 0700))
			case "file":
				writeFile(t, f.target, "foreign")
			case "dangling":
				must(t, os.Symlink("missing", f.target))
			}
			r := commandRunner{t.Context(), io.Discard}
			if err := r.moveDirectory(f.source, f.target, nil); !errors.Is(err, os.ErrExist) {
				t.Fatal("rename did not reject foreign entry:", err)
			}
			if readFile(t, filepath.Join(f.source, "notes.txt")) != "content" {
				t.Fatal("source renamed despite collision")
			}
			if kind == "file" && readFile(t, f.target) != "foreign" {
				t.Fatal("foreign file overwritten")
			}
		})
	}
}
