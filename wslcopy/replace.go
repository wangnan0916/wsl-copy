package wslcopy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A private, exclusively created sibling contains only this invocation's previous/failed trees.
// Reserving the container prevents accidental reuse of another invocation's backup name.
func (r commandRunner) replaceAndCopy(source, local string, target windowsPath, previous os.FileInfo, windowsTar, powershell string, output *Result) error {
	container, err := os.MkdirTemp(filepath.Dir(local), ".wsl-copy-")
	if err != nil {
		return err
	}
	containerInfo, err := os.Lstat(container)
	if err != nil {
		return errors.Join(err, os.Remove(container))
	}
	nativeContainer := strings.TrimRight(target.parent, `\`) + `\` + filepath.Base(container)
	old := filepath.Join(container, "previous")
	if err := r.ctx.Err(); err != nil {
		return errors.Join(err, os.Remove(container))
	}
	if err := r.moveDirectory(local, old, previous); err != nil {
		return errors.Join(fmt.Errorf("retire previous directory: %w", err), os.Remove(container))
	}
	output.PreviousWindowsPath = nativeContainer + `\previous`
	output.DestinationCreated, err = r.createAndCopy(source, local, target.native, windowsTar)
	if err != nil {
		if output.DestinationCreated {
			output.RetainedWindowsPath = target.native
		}
		// Cancellation may interrupt the WSL/Windows process bridge before the native
		// writer has stopped. Never restore old data into a possibly still-written path.
		if r.ctx.Err() != nil {
			return err
		}
		if output.DestinationCreated {
			partial := filepath.Join(container, "partial")
			if preserveErr := r.moveDirectory(local, partial, nil); preserveErr != nil {
				return errors.Join(err, fmt.Errorf("preserve failed output: %w", preserveErr))
			}
			output.RetainedWindowsPath = nativeContainer + `\partial`
		}
		if restoreErr := r.moveDirectory(old, local, previous); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous directory: %w", restoreErr))
		}
		output.PreviousWindowsPath = ""
		output.DestinationRestored = true
		if !output.DestinationCreated {
			// Only remove an empty reservation; never delete failed new output.
			if removeErr := os.Remove(container); removeErr != nil {
				return errors.Join(err, removeErr)
			}
		}
		return err
	}

	// Copy has committed. Launch one detached retirement process without waiting
	// for its tree checks/deletion. A launch failure cannot undo usable new output.
	if err := startRetirement(container, nativeContainer, containerInfo, previous, powershell); err != nil {
		output.CleanupError = fmt.Sprintf("start background retirement for %s: %v", nativeContainer, err)
	} else {
		output.PreviousWindowsPath = ""
	}
	return nil
}

// os.Rename rejects existing directories; the explicit Lstat also rejects files
// and dangling links. As with Copy, callers must keep these parent paths stable.
// These local rename syscalls are not killed by a canceled subprocess context.
func (r commandRunner) moveDirectory(from, to string, expected os.FileInfo) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (expected != nil && !os.SameFile(expected, info)) {
		return fmt.Errorf("directory identity/type changed: %s", from)
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("rename destination already exists: %s: %w", to, os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(r.stderr, "RENAME: %q -> %q\n", from, to)
	return os.Rename(from, to)
}

func startRetirement(container, native string, owner, previous os.FileInfo, powershell string) error {
	for path, expected := range map[string]os.FileInfo{container: owner, filepath.Join(container, "previous"): previous} {
		current, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !os.SameFile(expected, current) || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("retirement directory identity/type changed: %s", path)
		}
	}
	entries, err := os.ReadDir(container)
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != "previous" {
		return errors.New("retirement container contains unexpected entries")
	}
	target, err := destinationPath(native)
	if err != nil {
		return err
	}
	logPath := filepath.Join(container, "cleanup.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := log.WriteString("queued\n")
	if err := errors.Join(writeErr, log.Close()); err != nil {
		return err
	}
	script := nativePaths(target) + safeTreeScript + removePreviousScript
	if err := startRetirementProcess(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		// No child exists on Start failure; retain an operator diagnostic if possible.
		_ = os.WriteFile(logPath, []byte("failed: "+err.Error()+"\n"), 0600)
		return err
	}
	return nil
}
