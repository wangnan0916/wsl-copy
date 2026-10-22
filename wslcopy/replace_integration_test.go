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
	for _, mode := range []string{"success", "extract-failure", "readonly-cleanup", "junction-cleanup"} {
		t.Run(mode, func(t *testing.T) {
			f := newWindowsFixture(t, env)
			must(t, os.Mkdir(f.target, 0700))
			writeFile(t, filepath.Join(f.target, "old-only"), "previous content")
			opts := f.options(t)
			opts.Replace, opts.DryRun = true, true
			output, err := Copy(t.Context(), opts)
			must(t, err)
			if output.DestinationCreated || readFile(t, filepath.Join(f.target, "old-only")) != "previous content" {
				t.Fatal("replacement dry run mutated the destination")
			}
			opts.DryRun = false
			if mode == "extract-failure" {
				fail := filepath.Join(t.TempDir(), "fail")
				writeScript(t, fail, "echo injected-replacement-failure >&2\nexit 23\n")
				opts.WindowsTar = fail
			}
			if mode == "junction-cleanup" {
				sentinel := filepath.Join(f.root, "sentinel")
				must(t, os.Mkdir(sentinel, 0700))
				writeFile(t, filepath.Join(sentinel, "keep"), "untouched")
				script := "$old = @([IO.Directory]::GetDirectories(" + psQuote(f.nativeRoot) + ", '.wsl-copy-*')); " +
					"New-Item -ItemType Junction -Path ([IO.Path]::Combine($old[0], 'previous', 'injected-junction')) -Target " + psQuote(f.win(t, sentinel)) + " | Out-Null"
				extract := filepath.Join(t.TempDir(), "extract")
				t.Setenv("TEST_NATIVE_POWERSHELL", env.powershell)
				t.Setenv("TEST_NATIVE_TAR", env.tar)
				writeScript(t, extract, `"$TEST_NATIVE_POWERSHELL" -NoLogo -NoProfile -NonInteractive -Command '`+strings.ReplaceAll(script, "'", "'\\''")+"' </dev/null\nexec \"$TEST_NATIVE_TAR\" \"$@\"\n")
				opts.WindowsTar = extract
			}
			if mode == "readonly-cleanup" {
				_, err := env.ps(t.Context(), "[IO.File]::SetAttributes("+psQuote(`\\?\`+f.win(t, filepath.Join(f.target, "old-only")))+", [IO.FileAttributes]::ReadOnly)")
				must(t, err)
				t.Cleanup(func() {
					parent := f.win(t, f.target)
					if output.PreviousWindowsPath != "" {
						if !strings.HasPrefix(output.PreviousWindowsPath, f.nativeRoot+`\`) {
							t.Error("retained path escaped the fixture")
							return
						}
						parent = output.PreviousWindowsPath
					}
					_, err := env.ps(context.Background(), "[IO.File]::SetAttributes("+psQuote(`\\?\`+parent+`\old-only`)+", [IO.FileAttributes]::Normal)")
					if err != nil {
						t.Error(err)
					}
				})
			}
			output, err = Copy(t.Context(), opts)
			if mode == "junction-cleanup" {
				containers, globErr := filepath.Glob(filepath.Join(f.root, ".wsl-copy-*"))
				must(t, globErr)
				for _, container := range containers {
					junction := filepath.Join(container, "previous", "injected-junction")
					if _, statErr := os.Lstat(junction); statErr == nil {
						f.junctions = append(f.junctions, junction)
					}
				}
				if readFile(t, filepath.Join(f.root, "sentinel", "keep")) != "untouched" || !strings.Contains(output.CleanupError, "reparse point") {
					t.Fatalf("retirement did not reject injected junction: %+v; %v", output, err)
				}
			}
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
			if mode == "readonly-cleanup" || mode == "junction-cleanup" {
				if output.CleanupError == "" || output.PreviousWindowsPath == "" || output.DestinationRestored {
					t.Fatalf("cleanup failure did not preserve committed output: %+v", output)
				}
			} else {
				entries, err := os.ReadDir(f.root)
				must(t, err)
				if len(entries) != 1 || output.CleanupError != "" || output.PreviousWindowsPath != "" {
					t.Fatalf("native retirement left residue: %+v; %+v", entries, output)
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
	t.Run("nested-junction", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		must(t, os.Mkdir(f.target, 0700))
		sentinel := filepath.Join(f.root, "sentinel")
		must(t, os.Mkdir(sentinel, 0700))
		writeFile(t, filepath.Join(sentinel, "keep"), "untouched")
		junction := filepath.Join(f.target, "nested-junction")
		_, err := env.ps(t.Context(), "New-Item -ItemType Junction -Path "+psQuote(f.win(t, junction))+" -Target "+psQuote(f.win(t, sentinel))+" | Out-Null")
		must(t, err)
		f.junctions = append(f.junctions, junction)
		for _, dryRun := range []bool{false, true} {
			opts := f.options(t)
			opts.Replace, opts.DryRun = true, dryRun
			output, err := Copy(t.Context(), opts)
			if err == nil || !strings.Contains(err.Error(), "reparse point") || output.DestinationCreated {
				t.Fatalf("nested junction accepted: %+v; %v", output, err)
			}
		}
		if readFile(t, filepath.Join(sentinel, "keep")) != "untouched" {
			t.Fatal("junction sentinel changed")
		}
	})
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
