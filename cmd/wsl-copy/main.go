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
}

func parseOptions(args []string, output io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("wsl-copy", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.BoolVar(&opts.DryRun, "dry-run", false, "validate without creating a destination or running tar")
	fs.BoolVar(&opts.showVersion, "version", false, "print the version")
	fs.StringVar(&opts.WindowsTar, "windows-tar", os.Getenv("WSL_COPY_WINDOWS_TAR"), "WSL executable path (default: WSL_COPY_WINDOWS_TAR or tar.exe on PATH)")
	fs.StringVar(&opts.PowerShell, "powershell", os.Getenv("WSL_COPY_POWERSHELL"), "WSL executable path (default: WSL_COPY_POWERSHELL or powershell.exe on PATH)")
	fs.Usage = func() {
		fmt.Fprintln(output, "Usage: wsl-copy SOURCE WINDOWS_TARGET [options]\n\nCopy a WSL directory to a new Windows directory. Its parent must already exist.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.showVersion {
		return opts, nil
	}
	if fs.NArg() != 2 {
		return opts, errors.New("expected SOURCE and WINDOWS_TARGET; use --help for usage")
	}
	opts.Source, opts.WindowsTarget = fs.Arg(0), fs.Arg(1)
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
	output := result{Status: "failed", SourcePath: copied.SourcePath, WindowsPath: copied.WindowsPath}
	if err != nil {
		output.Error = err.Error()
		output.DestinationRetained = copied.DestinationCreated
		fmt.Fprintln(stderr, "wsl-copy:", err)
		if copied.DestinationCreated {
			fmt.Fprintln(stderr, "The destination was retained; no destination files were deleted.")
		}
	} else if opts.DryRun {
		output.Status = "dry_run"
	} else {
		output.Status = "copied"
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
