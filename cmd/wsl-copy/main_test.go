package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestMappingOptions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("WSL_COPY_WINDOWS_TAR", "")
	t.Setenv("WSL_COPY_POWERSHELL", "")
	must(t, os.Mkdir(filepath.Join(dir, "wsl-copy"), 0700))
	must(t, os.WriteFile(filepath.Join(dir, "wsl-copy", "config.json"), []byte(`{"source_root":"/source","windows_root":"Q:\\mirror","replace":true,"lock_file":"/state/config.lock","windows_tar":"/configured/tar.exe"}`), 0600))
	opts, err := parseOptions([]string{"/source/project"}, io.Discard)
	must(t, err)
	if opts.SourceRoot != "/source" || opts.WindowsRoot != `Q:\mirror` || !opts.Replace || opts.WindowsTar != "/configured/tar.exe" || opts.LockFile != "/state/config.lock" {
		t.Fatal("configuration not applied:", opts)
	}
	t.Setenv("WSL_COPY_WINDOWS_TAR", "/environment/tar.exe")
	opts, err = parseOptions([]string{"/source/project", `R:\explicit`, "--replace=false", "--source-root", "/override", "--lock-file", "/state/override.lock"}, io.Discard)
	must(t, err)
	if opts.Replace || opts.WindowsTarget != `R:\explicit` || opts.SourceRoot != "/override" || opts.WindowsTar != "/environment/tar.exe" || opts.LockFile != "/state/override.lock" {
		t.Fatal("explicit options or environment precedence lost:", opts)
	}
	opts, err = parseOptions([]string{"/source/project", `R:\explicit`, "--config=", "--windows-tar", "/flag/tar.exe", "--lock-file="}, io.Discard)
	must(t, err)
	if opts.Replace || opts.SourceRoot != "" || opts.WindowsTar != "/flag/tar.exe" || opts.LockFile != "" {
		t.Fatal("configuration not disabled or flag ignored:", opts)
	}
	if _, err := parseOptions([]string{"/source/project", "--config="}, io.Discard); err == nil {
		t.Fatal("missing target/mapping accepted")
	}
}

func TestOptions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("WSL_COPY_WINDOWS_TAR", "/env/tar.exe")
	t.Setenv("WSL_COPY_POWERSHELL", "/env/powershell.exe")
	for _, args := range [][]string{
		{"/source", `Q:\destination`, "--dry-run", "--windows-tar", "/explicit/tar.exe"},
		{"--dry-run", "--windows-tar=/explicit/tar.exe", "/source", `Q:\destination`},
		{"/source", "--windows-tar", "/explicit/tar.exe", `Q:\destination`, "--dry-run"},
		{"--dry-run", "--windows-tar", "/explicit/tar.exe", "--", "/source", `Q:\destination`},
	} {
		opts, err := parseOptions(args, io.Discard)
		must(t, err)
		want := options{Source: "/source", WindowsTarget: `Q:\destination`, WindowsTar: "/explicit/tar.exe", PowerShell: "/env/powershell.exe", DryRun: true, LockFile: filepath.Join(cache, "wsl-copy", "copy.lock")}
		if opts != want {
			t.Fatalf("incorrect options for %q: %+v", args, opts)
		}
	}
	opts, err := parseOptions([]string{"/source", `Q:\destination`}, io.Discard)
	must(t, err)
	if opts.WindowsTar != "/env/tar.exe" || opts.PowerShell != "/env/powershell.exe" {
		t.Fatal("environment defaults not used")
	}
	opts, err = parseOptions([]string{"/source", "--dry-run", "--dry-run=false", "--powershell=/explicit/powershell.exe", `Q:\destination`}, io.Discard)
	must(t, err)
	if opts.DryRun || opts.PowerShell != "/explicit/powershell.exe" {
		t.Fatalf("explicit option values not respected: %+v", opts)
	}
	opts, err = parseOptions([]string{"--windows-tar", "-literal", "--", "/source", "--dry-run"}, io.Discard)
	must(t, err)
	if opts.WindowsTar != "-literal" || opts.WindowsTarget != "--dry-run" || opts.DryRun {
		t.Fatalf("option value or terminator misparsed: %+v", opts)
	}
	for _, args := range [][]string{
		nil, {"/source"}, {"/source", `Q:\destination`, "extra"},
		{"/source", `Q:\destination`, "--unknown"}, {"--windows-tar"}, {"--powershell"},
		{"/source", `Q:\destination`, "--dry-run=invalid"}, {"/source", `Q:\destination`, "-dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("invalid arguments %q: exit %d, stdout %q, stderr %q", args, code, stdout.String(), stderr.String())
		}
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"/source", `Q:\destination`, "--help"}} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(args, &stdout, &stderr); code != 0 || strings.Count(stdout.String(), "Usage:") != 1 ||
			!strings.Contains(stdout.String(), "--dry-run") || stderr.Len() != 0 {
			t.Fatalf("incorrect help: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
		}
	}
	var versionOutput, versionError bytes.Buffer
	if code := runCLI([]string{"--version"}, &versionOutput, &versionError); code != 0 ||
		!strings.Contains(versionOutput.String(), version) || versionError.Len() != 0 {
		t.Fatal("version command failed")
	}
}
