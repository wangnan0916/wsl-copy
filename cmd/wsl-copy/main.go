package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/wangnan0916/wsl-copy/wslcopy"
)

var version = "dev"

type options struct {
	wslcopy.Options
	showVersion bool
}

type result struct {
	Status              string  `json:"status"`
	SourcePath          string  `json:"source_path,omitempty"`
	WindowsPath         string  `json:"windows_path,omitempty"`
	ElapsedSeconds      float64 `json:"elapsed_seconds"`
	Error               string  `json:"error,omitempty"`
	DestinationRetained bool    `json:"destination_retained,omitzero"`
	PreviousWindowsPath string  `json:"previous_windows_path,omitempty"`
	RetainedWindowsPath string  `json:"retained_windows_path,omitempty"`
	DestinationRestored bool    `json:"destination_restored,omitzero"`
	CleanupError        string  `json:"cleanup_error,omitempty"`
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("wsl-copy", flag.ContinueOnError)
	fs.SetOutput(output)
	var configPath string
	fs.StringVar(&configPath, "config", "", "JSON config path (default: user config directory/wsl-copy/config.json; empty disables)")
	fs.StringVar(&opts.SourceRoot, "source-root", "", "WSL root for automatic source-relative target mapping")
	fs.StringVar(&opts.WindowsRoot, "windows-root", "", "existing Windows root for automatic target mapping")
	fs.BoolVar(&opts.Replace, "replace", false, "rename an existing directory, copy, then retire the old directory in the background")
	fs.StringVar(&opts.LockFile, "lock-file", "", "shared WSL lock file (default: user cache/wsl-copy/copy.lock; explicit empty disables)")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "validate without creating, renaming or deleting directories or running tar")
	fs.BoolVar(&opts.showVersion, "version", false, "print the version")
	fs.StringVar(&opts.WindowsTar, "windows-tar", os.Getenv("WSL_COPY_WINDOWS_TAR"), "WSL executable path (default: WSL_COPY_WINDOWS_TAR or tar.exe on PATH)")
	fs.StringVar(&opts.PowerShell, "powershell", os.Getenv("WSL_COPY_POWERSHELL"), "WSL executable path (default: WSL_COPY_POWERSHELL or powershell.exe on PATH)")
	fs.Usage = func() {
		fmt.Fprintln(output, "Usage: wsl-copy SOURCE [WINDOWS_TARGET] [options]\n\nCopy a WSL directory to Windows. Specify a target or configure source-relative mapping.\nExisting targets are refused unless --replace or configuration enables replacement.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.showVersion {
		return opts, nil
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return opts, errors.New("expected SOURCE and optional WINDOWS_TARGET; use --help for usage")
	}
	cfg, err := loadConfig(configPath, fs.Changed("config"))
	if err != nil {
		return opts, err
	}
	for _, item := range []struct {
		name     string
		value    *string
		fallback string
	}{
		{"source-root", &opts.SourceRoot, cfg.SourceRoot}, {"windows-root", &opts.WindowsRoot, cfg.WindowsRoot},
		{"lock-file", &opts.LockFile, cfg.LockFile},
		{"windows-tar", &opts.WindowsTar, cfg.WindowsTar}, {"powershell", &opts.PowerShell, cfg.PowerShell},
	} {
		if !fs.Changed(item.name) && *item.value == "" {
			*item.value = item.fallback
		}
	}
	if !fs.Changed("replace") {
		opts.Replace = cfg.Replace
	}
	if !fs.Changed("lock-file") && opts.LockFile == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return opts, err
		}
		opts.LockFile = filepath.Join(dir, "wsl-copy", "copy.lock")
	}
	opts.Source, opts.WindowsTarget = fs.Arg(0), fs.Arg(1)
	if opts.WindowsTarget == "" && (opts.SourceRoot == "" || opts.WindowsRoot == "") {
		return opts, errors.New("expected WINDOWS_TARGET or both --source-root and --windows-root (also available in config)")
	}
	return opts, nil
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "wsl-copy:", err)
		return 2
	}
	if opts.showVersion {
		fmt.Fprintln(stdout, "wsl-copy", version)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	opts.Diagnostics = stderr
	copied, err := wslcopy.Copy(ctx, opts.Options)
	output := result{
		Status: "failed", SourcePath: copied.SourcePath, WindowsPath: copied.WindowsPath,
		PreviousWindowsPath: copied.PreviousWindowsPath, RetainedWindowsPath: copied.RetainedWindowsPath,
		DestinationRestored: copied.DestinationRestored, CleanupError: copied.CleanupError,
	}
	if err != nil {
		output.Error = err.Error()
		output.DestinationRetained = copied.DestinationCreated
		fmt.Fprintln(stderr, "wsl-copy:", err)
		if copied.RetainedWindowsPath != "" {
			fmt.Fprintln(stderr, "Failed output retained at:", copied.RetainedWindowsPath)
		} else if copied.DestinationCreated {
			fmt.Fprintln(stderr, "The destination was retained; no destination files were deleted.")
		}
		if copied.DestinationRestored {
			fmt.Fprintln(stderr, "The previous directory was restored.")
		}
	} else if opts.DryRun {
		output.Status = "dry_run"
	} else {
		output.Status = "copied"
	}
	if copied.PreviousWindowsPath != "" {
		fmt.Fprintln(stderr, "Previous data last known at:", copied.PreviousWindowsPath)
	}
	if copied.CleanupError != "" {
		fmt.Fprintln(stderr, "Copy completed; background retirement could not start:", copied.CleanupError)
	}
	output.ElapsedSeconds = math.Round(time.Since(start).Seconds()*1000) / 1000
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(stderr, "wsl-copy: write result:", err)
		return 1
	}
	if output.Status == "failed" {
		return 1
	}
	return 0
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}
