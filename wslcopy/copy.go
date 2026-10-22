// Package wslcopy copies WSL directories to new Windows directories without overwriting existing targets.
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

// Options configures a create-only copy.
type Options struct {
	// Source must be an absolute WSL filesystem directory.
	Source string
	// WindowsTarget must be an absent Windows drive path with an existing parent.
	WindowsTarget string
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
}

// Copy copies a WSL directory to a new Windows directory, or validates it in dry-run mode.
// The source and destination parent must remain stable until Copy returns.
// Result is meaningful even on error: a destination created by this call is never deleted.
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
	target, err := destinationPath(opts.WindowsTarget)
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
	if err := r.checkDestination(target, powershell); err != nil {
		return output, err
	}
	parent, err := r.convertPath(target.parent, "-u")
	if err != nil {
		return output, err
	}
	if !filepath.IsAbs(parent) {
		return output, errors.New("wslpath did not return an absolute destination parent path")
	}
	if err := checkDirectory(parent); err != nil {
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
	if _, err := os.Lstat(local); err == nil {
		return output, fmt.Errorf("destination already exists: %s", target.native)
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
