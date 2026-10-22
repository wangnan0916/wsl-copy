package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"
)

const installerURL = "https://raw.githubusercontent.com/wangnan0916/wsl-copy/main/install.sh"
const maxInstallerBytes = 64 << 10

func runUpdate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wsl-copy update", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: wsl-copy update\n\nInstall the newest public release (including prereleases)\nto $HOME/.local/bin/wsl-copy using the official HTTPS installer.\nConfiguration, copy targets and shell settings are unchanged. No sudo or Go required.\nA custom binary outside that location is not replaced. Output is text, not copy JSON.")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "wsl-copy update:", err)
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "wsl-copy update: no arguments accepted; updates to the newest public release")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	fmt.Fprintln(stdout, "Fetching the official wsl-copy installer...")
	if err := updateInstallation(ctx, installerURL, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "wsl-copy update:", err)
		return 1
	}
	return 0
}

func updateInstallation(ctx context.Context, url string, stdout, stderr io.Writer) error {
	// Download completely before execution; never run a partial curl | sh stream.
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("installer redirects are not allowed")
		},
	}
	var script []byte
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		request, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK || response.ContentLength > maxInstallerBytes {
			response.Body.Close()
			return fmt.Errorf("installer download refused: HTTP %d, content length %d", response.StatusCode, response.ContentLength)
		}
		if method == http.MethodGet {
			script, err = io.ReadAll(io.LimitReader(response.Body, maxInstallerBytes+1))
		}
		response.Body.Close()
		if err != nil {
			return err
		}
	}
	if len(script) == 0 || len(script) > maxInstallerBytes {
		return errors.New("installer is empty or exceeds 64 KiB")
	}
	// Reuse installer checksum/version checks, staged replacement and cleanup.
	cmd := exec.CommandContext(ctx, "sh", "-s", "--")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(string(script)), stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Let the installer run its traps; also terminate its active download child.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installer failed: %w", err)
	}
	return nil
}
