//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateArguments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"update", "--help"}, 0}, {[]string{"update", "-h"}, 0},
		{[]string{"update", "--unknown"}, 2}, {[]string{"update", "v0.2.1", "extra"}, 2},
		{[]string{"update", "https://example.com/script"}, 2}, {[]string{"update", "v0.2.1;echo bad"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(tc.args, &stdout, &stderr); code != tc.code {
			t.Fatalf("%q: exit %d, stdout %s, stderr %s", tc.args, code, &stdout, &stderr)
		}
		if tc.code == 0 && !strings.Contains(stdout.String(), "Usage: wsl-copy update [VERSION]") {
			t.Fatal("update help missing:", &stdout)
		}
	}
}

func TestUpdateCancellation(t *testing.T) {
	root := t.TempDir()
	started, stopped := filepath.Join(root, "started"), filepath.Join(root, "stopped")
	t.Setenv("TEST_UPDATE_STARTED", started)
	t.Setenv("TEST_UPDATE_STOPPED", stopped)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, "trap 'printf cleaned > \"$TEST_UPDATE_STOPPED\"; exit 143' TERM\nsleep 3 &\nprintf started > \"$TEST_UPDATE_STARTED\"\nwait\n")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- updateInstallation(ctx, server.URL, nil, io.Discard, io.Discard) }()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("installer did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled installer reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop installer and its child promptly")
	}
	data, err := os.ReadFile(stopped)
	must(t, err)
	if string(data) != "cleaned" {
		t.Fatal("installer cleanup trap was skipped")
	}
}

func TestUpdateDownloadGuards(t *testing.T) {
	for _, mode := range []string{"head-error", "head-size", "get-error", "stream-size", "truncated", "empty", "redirect", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "executed")
			t.Setenv("TEST_UPDATE_MARKER", marker)
			script := "printf bad > \"$TEST_UPDATE_MARKER\"\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					switch mode {
					case "head-error":
						w.WriteHeader(500)
					case "head-size":
						w.Header().Set("Content-Length", fmt.Sprint(maxInstallerBytes+1))
					case "redirect":
						http.Redirect(w, r, "/other", http.StatusFound)
					}
					return
				}
				switch mode {
				case "get-error":
					w.WriteHeader(404)
				case "stream-size":
					fmt.Fprint(w, script+strings.Repeat("#", maxInstallerBytes))
				case "truncated":
					w.Header().Set("Content-Length", fmt.Sprint(len(script)+10))
					fmt.Fprint(w, script)
				case "empty":
				default:
					fmt.Fprint(w, script)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if err := updateInstallation(ctx, server.URL, nil, io.Discard, io.Discard); err == nil {
				t.Fatal("unsafe/incomplete download accepted")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("installer executed before successful complete download")
			}
		})
	}
}
