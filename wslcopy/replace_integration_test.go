//go:build linux

package wslcopy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWindowsReplacement(t *testing.T, env windowsEnvironment) {
	for _, mode := range []string{"success", "extract-failure", "readonly-cleanup", "junction-cleanup", "existing-junction", "container-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			f := newWindowsFixture(t, env)
			must(t, os.Mkdir(f.target, 0700))
			writeFile(t, filepath.Join(f.target, "old-only"), "previous content")
			opts := f.options(t)
			opts.Replace = true
			if mode == "extract-failure" {
				fail := filepath.Join(t.TempDir(), "fail")
				writeScript(t, fail, "echo injected-replacement-failure >&2\nexit 23\n")
				opts.WindowsTar = fail
			}
			if mode == "junction-cleanup" || mode == "existing-junction" {
				sentinel := filepath.Join(f.root, "sentinel")
				must(t, os.Mkdir(sentinel, 0700))
				writeFile(t, filepath.Join(sentinel, "keep"), "untouched")
				if mode == "existing-junction" {
					_, err := env.ps(t.Context(), "New-Item -ItemType Junction -Path "+psQuote(f.win(t, filepath.Join(f.target, "nested-junction")))+" -Target "+psQuote(f.win(t, sentinel))+" | Out-Null")
					must(t, err)
				} else {
					script := "$old = @([IO.Directory]::GetDirectories(" + psQuote(f.nativeRoot) + ", '.wsl-copy-*')); " +
						"New-Item -ItemType Junction -Path ([IO.Path]::Combine($old[0], 'previous', 'nested-junction')) -Target " + psQuote(f.win(t, sentinel)) + " | Out-Null"
					extract := filepath.Join(t.TempDir(), "extract")
					t.Setenv("TEST_NATIVE_POWERSHELL", env.powershell)
					t.Setenv("TEST_NATIVE_TAR", env.tar)
					writeScript(t, extract, `"$TEST_NATIVE_POWERSHELL" -NoLogo -NoProfile -NonInteractive -Command '`+strings.ReplaceAll(script, "'", "'\\''")+"' </dev/null\nexec \"$TEST_NATIVE_TAR\" \"$@\"\n")
					opts.WindowsTar = extract
				}
				// Track the exact fixture junction even if replacement moved its parent.
				t.Cleanup(func() {
					if err := waitRetirements(f.root); err != nil {
						t.Error(err)
						return
					}
					paths, err := filepath.Glob(filepath.Join(f.root, ".wsl-copy-*", "previous", "nested-junction"))
					must(t, err)
					paths = append(paths, filepath.Join(f.target, "nested-junction"))
					for _, path := range paths {
						if _, err := os.Lstat(path); err == nil {
							f.junctions = append(f.junctions, path)
						}
					}
				})
			}
			if mode == "readonly-cleanup" {
				_, err := env.ps(t.Context(), "[IO.File]::SetAttributes("+psQuote(`\\?\`+f.win(t, filepath.Join(f.target, "old-only")))+", [IO.FileAttributes]::ReadOnly)")
				must(t, err)
				t.Cleanup(func() {
					if err := waitRetirements(f.root); err != nil {
						t.Error(err)
						return
					}
					paths, err := filepath.Glob(filepath.Join(f.root, ".wsl-copy-*", "previous", "old-only"))
					must(t, err)
					paths = append(paths, filepath.Join(f.target, "old-only"))
					for _, path := range paths {
						if _, err := os.Lstat(path); err == nil {
							_, err = env.ps(context.Background(), "[IO.File]::SetAttributes("+psQuote(`\\?\`+f.win(t, path))+", [IO.FileAttributes]::Normal)")
							must(t, err)
						}
					}
				})
			}
			if mode == "container-cleanup" {
				// Windows holds a directory handle for its process CWD: deleting
				// entries works, but final container removal must fail and recreate its log.
				t.Setenv("TEST_NATIVE_POWERSHELL", env.powershell)
				t.Setenv("TEST_RETIRE_PREFIX", "[Environment]::CurrentDirectory = @([IO.Directory]::GetDirectories("+psQuote(f.nativeRoot)+", '.wsl-copy-*'))[0]; ")
				wrapper := filepath.Join(t.TempDir(), "powershell")
				writeScript(t, wrapper, `
for script; do :; done
case "$script" in
  *'Refusing an unrecognized retirement directory'*)
    exec "$TEST_NATIVE_POWERSHELL" -NoLogo -NoProfile -NonInteractive -Command "$TEST_RETIRE_PREFIX$script" ;;
  *) exec "$TEST_NATIVE_POWERSHELL" "$@" ;;
esac
`)
				opts.PowerShell = wrapper
			}
			opts.DryRun = true
			output, err := Copy(t.Context(), opts)
			must(t, err)
			if output.DestinationCreated || readFile(t, filepath.Join(f.target, "old-only")) != "previous content" {
				t.Fatal("replacement dry run mutated the destination")
			}
			opts.DryRun = false
			output, err = Copy(t.Context(), opts)
			if !output.DestinationCreated {
				t.Fatalf("replacement did not create a new destination: %+v; %v", output, err)
			}
			if mode == "extract-failure" {
				if err == nil || err.Error() != "fail failed (23): injected-replacement-failure" || !output.DestinationRestored || output.RetainedWindowsPath == "" || output.PreviousWindowsPath != "" {
					t.Fatalf("native rollback failed: %+v; %v", output, err)
				}
				if readFile(t, filepath.Join(f.target, "old-only")) != "previous content" {
					t.Fatal("previous target not restored")
				}
				return
			}
			must(t, err)
			assertTree(t, f.source, f.target)
			if output.CleanupError != "" || output.PreviousWindowsPath != "" || output.DestinationRestored {
				t.Fatalf("unexpected foreground recovery state: %+v", output)
			}
			must(t, waitRetirements(f.root))
			containers, err := filepath.Glob(filepath.Join(f.root, ".wsl-copy-*"))
			must(t, err)
			if mode != "success" {
				if len(containers) != 1 {
					t.Fatal("failed worker did not retain its container")
				}
				log := readFile(t, filepath.Join(containers[0], "cleanup.log"))
				if !strings.Contains(log, "failed:") {
					t.Fatal("worker failure not recorded:", log)
				}
				if mode == "container-cleanup" {
					absent(t, filepath.Join(containers[0], "previous"))
				}
				if (mode == "junction-cleanup" || mode == "existing-junction") && (!strings.Contains(log, "reparse point") || readFile(t, filepath.Join(f.root, "sentinel", "keep")) != "untouched") {
					t.Fatal("worker followed a junction or failed to report its rejection:", log)
				}
			} else {
				if len(containers) != 0 {
					t.Fatal("successful retirement left residue")
				}
				writeFile(t, filepath.Join(f.source, "notes.txt"), "second copy")
				output, err = Copy(t.Context(), opts)
				must(t, err)
				assertTree(t, f.source, f.target)
				if output.CleanupError != "" {
					t.Fatal(output.CleanupError)
				}
			}
		})
	}
	t.Run("mapped-parents", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		sourceRoot := t.TempDir()
		f.source = filepath.Join(sourceRoot, "project", "dist")
		must(t, os.MkdirAll(f.source, 0700))
		writeFile(t, filepath.Join(f.source, "notes.txt"), "mapped")
		f.target = filepath.Join(f.root, "project", "dist")
		opts := f.options(t)
		opts.WindowsTarget, opts.SourceRoot = "", sourceRoot
		opts.WindowsRoot = f.nativeRoot + `\missing-root`
		for _, dryRun := range []bool{false, true} {
			opts.DryRun = dryRun
			if _, err := Copy(t.Context(), opts); err == nil {
				t.Fatal("missing mapping root was accepted")
			}
			absent(t, filepath.Join(f.root, "missing-root"))
		}
		opts.WindowsRoot, opts.DryRun = f.nativeRoot, true
		output, err := Copy(t.Context(), opts)
		must(t, err)
		if output.WindowsPath != f.win(t, f.target) {
			t.Fatal("wrong mapped path:", output)
		}
		absent(t, filepath.Dir(f.target))
		opts.DryRun = false
		_, err = Copy(t.Context(), opts)
		must(t, err)
		assertTree(t, f.source, f.target)
	})
}
