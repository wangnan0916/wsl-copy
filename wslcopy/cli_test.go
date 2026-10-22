//go:build linux

package wslcopy

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise the real CLI protocol; copying and filesystem safety belong to library tests.
func TestCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "wsl-copy")
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.version=cli-test", "-o", binary, "../cmd/wsl-copy")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	if output, err := exec.CommandContext(t.Context(), binary, "--version").CombinedOutput(); err != nil || string(output) != "wsl-copy cli-test\n" {
		t.Fatalf("linked version not reported: %v; %q", err, output)
	}
	for _, mode := range []string{"copy", "dry-run", "invalid-source", "prepare-failure", "extract-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newLocalFixture(t)
			// Explicit fixture tools ensure ambient CLI environment cannot invoke Windows.
			args := []string{f.source, f.native, "--windows-tar", filepath.Join(f.bin, "tar.exe"), "--powershell", filepath.Join(f.bin, "powershell.exe")}
			status, wantCode := "failed", 1
			switch mode {
			case "copy":
				status, wantCode = "copied", 0
			case "dry-run":
				args = append(args, "--dry-run")
				status, wantCode = "dry_run", 0
			case "invalid-source":
				args[0] = filepath.Join(f.source, "missing\xff")
			case "prepare-failure":
				t.Setenv("TEST_NATIVE_ERROR", "1")
			case "extract-failure":
				t.Setenv("TEST_EXTRACT_FAIL", "1")
			}
			cmd := exec.CommandContext(t.Context(), binary, args...)
			cmd.Dir = f.parent // No source checkout or external PowerShell script here.
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			code := 0
			if err := cmd.Run(); err != nil {
				exit, ok := errors.AsType[*exec.ExitError](err)
				if !ok {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			var output map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatalf("invalid JSON: %v; stdout %q; stderr %q", err, stdout.String(), stderr.String())
			}
			if elapsed, ok := output["elapsed_seconds"].(float64); !ok || elapsed < 0 {
				t.Fatal("invalid elapsed_seconds:", output)
			}
			want := map[string]any{"status": status, "elapsed_seconds": output["elapsed_seconds"]}
			if mode != "invalid-source" {
				want["source_path"], want["windows_path"] = f.source, f.native
			}
			if wantCode == 1 {
				message, ok := output["error"].(string)
				if !ok || message == "" || (mode == "invalid-source" && !strings.Contains(message, "missing\ufffd")) {
					t.Fatal("missing error or invalid UTF-8 was not replaced:", output)
				}
				want["error"] = message
			}
			if mode == "extract-failure" {
				want["error"] = "tar.exe failed (2): simulated extraction failure"
				want["destination_retained"] = true
				if !strings.Contains(stderr.String(), "The destination was retained") {
					t.Fatal("missing retention diagnostics:", stderr.String())
				}
			}
			if code != wantCode || !reflect.DeepEqual(output, want) {
				t.Fatalf("wrong CLI protocol: exit %d (want %d), got %+v, want %+v; stderr %s", code, wantCode, output, want, stderr.String())
			}
		})
	}
}
