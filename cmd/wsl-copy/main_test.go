package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOptions(t *testing.T) {
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
		want := options{Source: "/source", WindowsTarget: `Q:\destination`, WindowsTar: "/explicit/tar.exe", PowerShell: "/env/powershell.exe", DryRun: true}
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
		{"/source", `Q:\destination`, "--replace"}, {"--windows-tar"}, {"--powershell"},
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
