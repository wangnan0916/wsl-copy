//go:build linux

package wslcopy

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"
)

type windowsEnvironment struct {
	tar, powershell, temp, tarVersion string
}

type windowsFixture struct {
	env                              windowsEnvironment
	source, root, nativeRoot, target string
	junctions                        []string
}

func (e windowsEnvironment) ps(ctx context.Context, script string) ([]byte, error) {
	r := commandRunner{ctx, io.Discard}
	return r.run(nil, nil, e.powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		"$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new($false); "+script)
}

func newWindowsFixture(t *testing.T, env windowsEnvironment) *windowsFixture {
	t.Helper()
	f := &windowsFixture{env: env, source: t.TempDir()}
	f.nativeRoot = strings.TrimRight(env.temp, `\`) + `\wsl-copy-test-` + uuid.New().String()
	r := commandRunner{t.Context(), io.Discard}
	root, err := r.convertPath(f.nativeRoot, "-u")
	must(t, err)
	f.root = root
	must(t, os.Mkdir(f.root, 0700))
	t.Cleanup(func() {
		// t.Context() is already canceled; cleanup needs an independent context.
		// Delete exact test-created junctions nonrecursively before removing this owned fixture.
		for _, junction := range slices.Backward(f.junctions) {
			_, err := env.ps(context.Background(), "[IO.Directory]::Delete("+psQuote(`\\?\`+f.win(t, junction))+", $false)")
			if err != nil {
				t.Error(err)
				return // Never recurse if a junction could not be removed.
			}
		}
		if err := os.RemoveAll(f.root); err != nil {
			t.Error(err)
		}
	})
	f.target = filepath.Join(f.root, "中文 O'Brien [destination]")
	writeFile(t, filepath.Join(f.source, "notes.txt"), "content")
	return f
}

func (f *windowsFixture) win(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(f.root, path)
	must(t, err)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		t.Fatal("fixture path escaped its root")
	}
	if rel == "." {
		return f.nativeRoot
	}
	// Literal mapping: wslpath may resolve junctions and must not be used for teardown.
	return f.nativeRoot + `\` + strings.ReplaceAll(rel, "/", `\`)
}

func (f *windowsFixture) options(t *testing.T) Options {
	t.Helper()
	return Options{
		Source: f.source, WindowsTarget: f.win(t, f.target),
		WindowsTar: f.env.tar, PowerShell: f.env.powershell,
	}
}

func (f *windowsFixture) skipKnownFailure(t *testing.T, output Result, err error, symptom string) {
	t.Helper()
	info, statErr := os.Stat(f.target)
	if strings.HasPrefix(f.env.tarVersion, "bsdtar 3.8.9 ") && err != nil &&
		output.DestinationCreated && statErr == nil && info.IsDir() &&
		strings.HasPrefix(err.Error(), filepath.Base(f.env.tar)+" failed (1):") && strings.Contains(err.Error(), symptom) {
		t.Skip("Known Windows tar 3.8.9 limitation:", symptom)
	}
}

func TestWindows(t *testing.T) {
	if os.Getenv("WSL_COPY_WINDOWS_TESTS") != "1" {
		t.Skip("Set WSL_COPY_WINDOWS_TESTS=1 to run native Windows fixture tests")
	}
	var env windowsEnvironment
	var err error
	env.tar, err = findTool(cmp.Or(os.Getenv("WSL_COPY_WINDOWS_TAR"), "tar.exe"))
	must(t, err)
	env.powershell, err = findTool(cmp.Or(os.Getenv("WSL_COPY_POWERSHELL"), "powershell.exe"))
	must(t, err)
	output, err := env.ps(t.Context(), "[IO.Path]::GetTempPath()")
	must(t, err)
	env.temp = strings.TrimRight(string(output), "\r\n")
	r := commandRunner{t.Context(), io.Discard}
	output, err = r.run(nil, nil, env.tar, "--version")
	must(t, err)
	env.tarVersion = string(output)

	t.Run("replacement", func(t *testing.T) { testWindowsReplacement(t, env) })
	t.Run("content", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		writeFile(t, filepath.Join(f.source, "binary"), "\x00\xffpayload\r\n")
		must(t, os.Link(filepath.Join(f.source, "binary"), filepath.Join(f.source, "hardlink")))
		writeFile(t, filepath.Join(f.source, ".hidden"), "included")
		must(t, os.MkdirAll(filepath.Join(f.source, "中文 O'Brien", "empty"), 0700))
		old := time.Unix(946684800, 0)
		must(t, os.Chtimes(filepath.Join(f.source, "binary"), old, old))
		output, err := Copy(t.Context(), f.options(t))
		if err != nil || !output.DestinationCreated || output.WindowsPath != f.win(t, f.target) {
			t.Fatalf("copy failed: %+v; %v", output, err)
		}
		assertTree(t, f.source, f.target)
		binary, err := os.Stat(filepath.Join(f.target, "binary"))
		must(t, err)
		hardlink, err := os.Stat(filepath.Join(f.target, "hardlink"))
		must(t, err)
		if os.SameFile(binary, hardlink) || binary.ModTime().Unix() == 946684800 {
			t.Fatal("hard links or source timestamps were preserved")
		}
	})
	t.Run("existing-targets", func(t *testing.T) {
		for _, kind := range []string{"file", "empty", "nonempty"} {
			f := newWindowsFixture(t, env)
			if kind == "file" {
				writeFile(t, f.target, "keep file")
			} else {
				must(t, os.Mkdir(f.target, 0700))
				if kind == "nonempty" {
					writeFile(t, filepath.Join(f.target, "sentinel"), "keep directory")
				}
			}
			for _, dryRun := range []bool{false, true} {
				opts := f.options(t)
				opts.DryRun = dryRun
				output, err := Copy(t.Context(), opts)
				if err == nil || output.DestinationCreated || !strings.Contains(err.Error(), "Destination already exists") {
					t.Fatalf("existing %s not refused by native validation (dry-run=%t): %+v; %v", kind, dryRun, output, err)
				}
			}
			if kind == "file" {
				if readFile(t, f.target) != "keep file" {
					t.Fatal("existing file modified")
				}
			} else {
				if kind == "nonempty" && readFile(t, filepath.Join(f.target, "sentinel")) != "keep directory" {
					t.Fatal("existing directory modified")
				}
				absent(t, filepath.Join(f.target, "notes.txt"))
			}
		}
	})
	t.Run("missing-parent", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		f.target = filepath.Join(f.root, "missing", "destination")
		if output, err := Copy(t.Context(), f.options(t)); err == nil || output.DestinationCreated {
			t.Fatalf("missing parent accepted: %+v; %v", output, err)
		}
		absent(t, filepath.Dir(f.target))
	})
	t.Run("junctions", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		sentinel := filepath.Join(f.root, "sentinel")
		must(t, os.Mkdir(sentinel, 0700))
		writeFile(t, filepath.Join(sentinel, "keep"), "untouched")
		link := filepath.Join(f.root, "中文 O'Brien junction")
		_, err := env.ps(t.Context(), "New-Item -ItemType Junction -Path "+psQuote(f.win(t, link))+" -Target "+psQuote(f.win(t, sentinel))+" | Out-Null")
		must(t, err)
		f.junctions = append(f.junctions, link)
		for _, dryRun := range []bool{false, true} {
			f.target = link
			opts := f.options(t)
			opts.DryRun = dryRun
			if output, err := Copy(t.Context(), opts); err == nil || output.DestinationCreated || !strings.Contains(err.Error(), "Destination already exists") {
				t.Fatalf("junction target not refused (dry-run=%t): %+v; %v", dryRun, output, err)
			}
			f.target = filepath.Join(link, "destination")
			opts = f.options(t)
			opts.DryRun = dryRun
			if output, err := Copy(t.Context(), opts); err == nil || output.DestinationCreated || !strings.Contains(err.Error(), "reparse point") {
				t.Fatalf("junction ancestor not refused (dry-run=%t): %+v; %v", dryRun, output, err)
			}
		}
		if readFile(t, filepath.Join(sentinel, "keep")) != "untouched" {
			t.Fatal("junction target modified")
		}
		absent(t, filepath.Join(sentinel, "destination"))
	})
	t.Run("failure-extract", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		fail := filepath.Join(t.TempDir(), "fail")
		writeScript(t, fail, "echo injected-extract-failure >&2\nexit 23\n")
		opts := f.options(t)
		opts.WindowsTar = fail
		output, err := Copy(t.Context(), opts)
		f.skipKnownFailure(t, output, err, "Archive entry has empty or unreadable filename")
		if err == nil || err.Error() != "fail failed (23): injected-extract-failure" || !output.DestinationCreated {
			t.Fatalf("wrong failure stage or ownership: %+v; %v", output, err)
		}
		if info, err := os.Stat(f.target); err != nil || !info.IsDir() {
			t.Fatal("new Windows destination not retained:", err)
		}
	})
	t.Run("emoji", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		name := "sample-😀.txt"
		writeFile(t, filepath.Join(f.source, name), "emoji")
		output, err := Copy(t.Context(), f.options(t))
		f.skipKnownFailure(t, output, err, "Archive entry has empty or unreadable filename")
		if err != nil || readFile(t, filepath.Join(f.target, name)) != "emoji" {
			t.Fatalf("emoji filename not preserved: %+v; %v", output, err)
		}
	})
	t.Run("decomposed-unicode", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		name := "cafe\u0301.txt"
		writeFile(t, filepath.Join(f.source, name), "decomposed")
		output, err := Copy(t.Context(), f.options(t))
		if err != nil || !output.DestinationCreated {
			t.Fatalf("copy failed: %+v; %v", output, err)
		}
		entries, err := os.ReadDir(f.target)
		must(t, err)
		if strings.HasPrefix(env.tarVersion, "bsdtar 3.8.9 ") && len(entries) == 2 && entries[0].Name() == "café.txt" && entries[1].Name() == "notes.txt" {
			if readFile(t, filepath.Join(f.target, "café.txt")) != "decomposed" {
				t.Fatal("content mismatch, not a known normalization issue")
			}
			t.Skip("Known Windows tar 3.8.9 limitation: NFD filename normalized to NFC")
		}
		if readFile(t, filepath.Join(f.target, name)) != "decomposed" {
			t.Fatal("decomposed filename not preserved")
		}
	})
	t.Run("long-destination", func(t *testing.T) {
		f := newWindowsFixture(t, env)
		parent := f.root
		for i := range 8 {
			parent = filepath.Join(parent, fmt.Sprintf("parent-%d-%s", i, strings.Repeat("x", 30)))
			must(t, os.Mkdir(parent, 0700))
		}
		f.target = filepath.Join(parent, "destination")
		output, err := Copy(t.Context(), f.options(t))
		f.skipKnownFailure(t, output, err, "could not chdir to '"+f.win(t, f.target)+"'")
		if err != nil || readFile(t, filepath.Join(f.target, "notes.txt")) != "content" {
			t.Fatalf("long destination failed: %+v; %v", output, err)
		}
	})
}
