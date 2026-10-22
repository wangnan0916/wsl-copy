// Package wslcopy copies WSL directories to Windows, with optional path mapping and directory replacement.
package wslcopy

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Options configures copying. Existing destinations are refused unless Replace is enabled.
type Options struct {
	// Source must be an absolute WSL filesystem directory.
	Source string
	// WindowsTarget is an explicit Windows drive path with an existing parent.
	WindowsTarget string
	// SourceRoot and WindowsRoot map source-relative paths when WindowsTarget is empty.
	// Both roots must exist; missing parents below WindowsRoot may be created.
	SourceRoot, WindowsRoot string
	// Replace permits retiring an existing ordinary directory before copying.
	Replace bool
	// WSL executable paths; empty values resolve tar.exe and powershell.exe through PATH.
	WindowsTar, PowerShell string
	// DryRun validates without creating a destination or running tar.
	DryRun bool
	// Diagnostics receives command logs and subprocess stderr; nil discards them.
	Diagnostics io.Writer
}

// Result describes validated paths and destination ownership, including on failure.
type Result struct {
	SourcePath, WindowsPath string
	// DestinationCreated is true only if this call created the destination.
	// It remains true if packing or extraction subsequently fails.
	DestinationCreated bool
	// PreviousWindowsPath is the last-known location of retired data, if retained.
	// Cleanup failure may leave this tree only partly present.
	PreviousWindowsPath string
	// RetainedWindowsPath locates failed new output, including after rollback.
	RetainedWindowsPath string
	DestinationRestored bool
	// CleanupError is a warning after a successful copy, not a reason to roll back.
	CleanupError string
}

// Copy copies a WSL directory to Windows, or validates it in dry-run mode.
// Keep the source, destination parent and retired tree stable until Copy returns.
// Failed new output is never deleted. Replacement may restore the old directory
// and move failed output to RetainedWindowsPath. Inspect Result even on error.
func Copy(ctx context.Context, opts Options) (Result, error) {
	var output Result
	r := commandRunner{ctx, cmp.Or(opts.Diagnostics, io.Discard)}
	if runtime.GOOS != "linux" {
		return output, errors.New("wsl-copy must run inside WSL as a Linux executable")
	}
	source, err := sourcePath(opts.Source)
	if err != nil {
		return output, err
	}
	target, mappingRoot, err := resolveDestination(source, opts)
	if err != nil {
		return output, err
	}
	output.SourcePath, output.WindowsPath = source, target.native
	tools := []string{cmp.Or(opts.WindowsTar, "tar.exe"), cmp.Or(opts.PowerShell, "powershell.exe"), "tar", "wslpath"}
	for i, name := range tools {
		tools[i], err = findTool(name)
		if err != nil {
			return output, err
		}
	}
	windowsTar, powershell := tools[0], tools[1]
	sourceNative, err := r.convertPath(source, "-w")
	if err != nil {
		return output, err
	}
	wslRoot, err := r.convertPath("/", "-w")
	if err != nil {
		return output, err
	}
	volume := uncVolume(wslRoot)
	if volume == "" || !strings.EqualFold(uncVolume(sourceNative), volume) {
		return output, errors.New("source must be on the WSL filesystem, not a Windows drive or network share")
	}
	if err := r.checkDestination(target, powershell, opts.Replace, mappingRoot); err != nil {
		return output, err
	}
	parent, err := r.convertPath(target.parent, "-u")
	if err != nil {
		return output, err
	}
	if !filepath.IsAbs(parent) {
		return output, errors.New("wslpath did not return an absolute destination parent path")
	}
	if err := checkDirectory(parent, mappingRoot != ""); err != nil {
		return output, err
	}
	mapped, err := r.convertPath(parent, "-w")
	if err != nil {
		return output, err
	}
	if !strings.EqualFold(mapped, target.parent) {
		return output, errors.New("destination parent does not map to the requested Windows directory")
	}
	local := filepath.Join(parent, target.leaf)
	previous, err := os.Lstat(local)
	if err == nil {
		if !opts.Replace {
			return output, fmt.Errorf("destination already exists: %s", target.native)
		}
		if !previous.IsDir() || previous.Mode()&os.ModeSymlink != 0 {
			return output, errors.New("replacement requires an ordinary directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return output, err
	}
	if within(source, local) || within(local, source) {
		return output, errors.New("source and destination must not overlap")
	}
	if err := inspectSource(source); err != nil {
		return output, err
	}
	if opts.DryRun {
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	if mappingRoot != "" {
		if err := os.MkdirAll(parent, 0777); err != nil {
			return output, err
		}
	}
	if previous != nil {
		err := r.replaceAndCopy(source, local, target, previous, windowsTar, powershell, &output)
		return output, err
	}
	output.DestinationCreated, err = r.createAndCopy(source, local, target.native, windowsTar)
	return output, err
}

// The final write boundary must reject a destination that appeared after preflight.
func (r commandRunner) createAndCopy(source, local, nativeTarget, windowsTar string) (bool, error) {
	if err := os.Mkdir(local, 0777); err != nil {
		return false, err
	}
	return true, r.copyArchive(source, nativeTarget, windowsTar)
}
