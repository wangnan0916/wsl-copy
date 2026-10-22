# wsl-copy — WSL to Windows directory copy

**Copy WSL directories to new Windows directories. No overwrite, no sync.**

`wsl-copy` is a standalone Linux CLI and reusable Go library for copying folders from Windows Subsystem for Linux (WSL2) to Windows. Transfers use one uncompressed GNU tar archive and one Windows tar extraction process.

Use it to copy build artifacts, front-end bundles or WeChat mini-program output from the WSL filesystem to Windows tools. The source stays unchanged, and the destination must be new. It does not build projects, watch for changes or control Windows applications.

[Quick start](#quick-start-copy-a-wsl-folder-to-windows) · [Go library](#go-library-api-and-project-layout) · [Safety](#create-only-copying-and-safety) · [Benchmarks](#performance-benchmarks) · [FAQ](#faq)

## Quick start: copy a WSL folder to Windows

### Install in WSL (no Go required)

[v0.1.0](https://github.com/wangnan0916/wsl-copy/releases/tag/v0.1.0) is the initial preview release. Run the following in a **WSL terminal**, not Windows PowerShell. It selects the architecture, verifies SHA-256 checksums and installs for the current user without `sudo`:

```bash
(
  set -eu
  version=v0.1.0
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "Unsupported architecture" >&2; exit 1 ;;
  esac
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  cd "$work"
  base="https://github.com/wangnan0916/wsl-copy/releases/download/$version"
  curl -fLO "$base/wsl-copy-linux-$arch.tar.gz"
  curl -fLO "$base/SHA256SUMS"
  sha256sum --ignore-missing -c SHA256SUMS
  tar -xzf "wsl-copy-linux-$arch.tar.gz"
  install -Dm755 wsl-copy "$HOME/.local/bin/wsl-copy"
  install -Dm644 LICENSE "$HOME/.local/share/wsl-copy/LICENSE"
  install -Dm644 THIRD_PARTY_NOTICES "$HOME/.local/share/wsl-copy/THIRD_PARTY_NOTICES"
  "$HOME/.local/bin/wsl-copy" --version
)
```

The executable is installed at `~/.local/bin/wsl-copy`; license notices are kept in `~/.local/share/wsl-copy/`. The PATH command below applies to the current shell; add it to your shell startup file if `~/.local/bin` is not already on your PATH. To upgrade, change `version` to the desired published tag and rerun the installation block; it replaces the installed executable and license notices. There is no automatic updater.

Then copy an existing WSL directory to a **new** Windows directory:

```bash
export PATH="$HOME/.local/bin:$PATH"
wsl-copy "$HOME/project/dist" 'D:\Copies\my-build'
```

| WSL architecture | Archive | v0.1.0 validation |
| --- | --- | --- |
| x86-64 (`uname -m`: `x86_64`) | `wsl-copy-linux-amd64.tar.gz` | Tested with native Windows tools |
| ARM64 (`uname -m`: `aarch64`) | `wsl-copy-linux-arm64.tar.gz` | Cross-built only; not tested on ARM64 hardware |

For manual installation, download the matching archive and `SHA256SUMS` from [Releases](https://github.com/wangnan0916/wsl-copy/releases). Each archive contains `wsl-copy`, `LICENSE` and `THIRD_PARTY_NOTICES`. These are Linux binaries, not Windows `.exe` files. See [Windows tool paths](#windows-tool-paths) if dependencies are not on your PATH, and [known Windows tar limitations](#known-windows-tar-limitations) before copying affected names or paths.

### Build from source

Clone [wangnan0916/wsl-copy](https://github.com/wangnan0916/wsl-copy), then build inside WSL with Go 1.27.3 or newer:

```bash
make build
./dist/wsl-copy --version
./dist/wsl-copy "$HOME/project/dist" 'D:\Copies\my-build'
```

Without Make: `CGO_ENABLED=0 go build -trimpath -o dist/wsl-copy ./cmd/wsl-copy`.

The result is a standalone Linux executable. Running it does not require Go or Python, but still requires:

- WSL with Windows executable interoperability.
- GNU `tar` and `wslpath` on the WSL `PATH`.
- Windows `tar.exe` and Windows PowerShell, available through `PATH` or explicit paths.
- A destination volume mounted and accessible from WSL.

Argument parsing uses [pflag](https://github.com/spf13/pflag), and Windows filename checks use [muonsoft/validation](https://github.com/muonsoft/validation). Archive transfer uses the Go standard library and system tools. No username, distribution, drive letter, mount point or mirror directory is hardcoded.

Replace the example paths with your own. The source must be an existing absolute WSL filesystem directory. The Windows destination must not exist, even as an empty directory or dangling link; its parent must already exist. Quote Windows paths in your shell.

Single files, Windows-mounted sources, UNC/device destinations, drive roots, `..` components, symbolic-link paths and junction ancestors are not supported.

`--dry-run` validates paths and source entries without creating a destination or running tar. It does not prove extraction compatibility. Options may appear before, between or after the two paths; `--` ends option parsing. Long options use GNU syntax (`--dry-run`, not `-dry-run`). Use `--help` or `-h` for help.

### Windows tool paths

If Windows executables are not on your WSL `PATH`, supply their **WSL executable paths**:

```bash
./dist/wsl-copy "$HOME/project/dist" 'D:\Copies\my-build' \
  --windows-tar /path/to/tar.exe \
  --powershell /path/to/powershell.exe
```

Resolution order: explicit options, then `WSL_COPY_WINDOWS_TAR` / `WSL_COPY_POWERSHELL`, then `PATH`. Missing dependencies are reported, not installed or searched for in guessed drive locations.

## Go library API and project layout

The public package `github.com/wangnan0916/wsl-copy/wslcopy` (package name `wslcopy`) exposes the copy operation independently of the CLI:

```bash
go get github.com/wangnan0916/wsl-copy/wslcopy@v0.1.0
```

```go
func Copy(ctx context.Context, opts Options) (Result, error)
```

```go
import "github.com/wangnan0916/wsl-copy/wslcopy"

result, err := wslcopy.Copy(ctx, wslcopy.Options{
    Source: source,
    WindowsTarget: target,
})
```

`Result` contains `SourcePath`, `WindowsPath` and `DestinationCreated`. Inspect it even when `err` is non-nil: once this call creates the destination, `DestinationCreated` stays true, including after a packing or extraction failure. The library never removes that destination. `Options.DryRun` validates without creation or tar execution.

`Options.WindowsTar` and `Options.PowerShell` accept WSL executable paths; empty values use `PATH`. Only the CLI reads the `WSL_COPY_*` environment defaults. The library accepts the caller's context, installs no signal handlers, and does not write JSON, select exit codes or use process stdout/stderr directly. Set `Options.Diagnostics` to an `io.Writer` to receive command logs and subprocess diagnostics; nil discards them.

```text
.
├── .github/workflows/release.yml  # CI builds and tag-triggered releases
├── cmd/wsl-copy/
│   ├── main.go                    # Arguments, signals and CLI output
│   └── main_test.go               # Argument, help and version tests
├── wslcopy/
│   ├── copy.go, copy_test.go       # Public API and copy orchestration
│   ├── paths.go, paths_test.go     # Path, filename and source-tree validation
│   ├── command.go, command_test.go # Subprocesses, Windows checks and archive transfer
│   ├── fixtures_test.go           # Shared test fixtures and assertions
│   ├── cli_test.go                # Compiled CLI/library boundary tests
│   ├── integration_test.go        # Opt-in native Windows tests
│   └── scripts/check-destination.ps1 # Embedded read-only Windows checks
├── README.md
├── LICENSE, THIRD_PARTY_NOTICES
├── Makefile
└── go.mod, go.sum
```

The PowerShell script is embedded at build time, not loaded from the working directory. No separate script file is needed at runtime; retain the accompanying license notices when redistributing the binary.

## Create-only copying and safety

- Read-only Windows checks reject existing destinations and reparse-point ancestors before path conversion. The converted parent is mapped back to Windows for validation. An exclusive `mkdir` prevents entering a destination created by another caller.
- The source is checked once for regular files/directories and case-insensitive collisions using Unicode simple folding. Source names and destination components use conservative Windows filename validation on every host OS: invalid UTF-8, control characters, reserved device names (including `CONIN$` and `CONOUT$`), and trailing dots/spaces are rejected. This does not normalize filenames or prove extraction compatibility.
- GNU tar creates one uncompressed POSIX archive in WSL temporary storage. `TAR_OPTIONS` is removed from the packing environment. The archive must be outside the source; set `TMPDIR` to another writable directory if necessary.
- Archive bytes go directly through binary stdin to Windows tar, not through a PowerShell text pipeline. Hidden files and empty directories are included. Hard links become independent content. `-m` prevents restoration of source modification times; Linux ownership, permissions, ACLs and executable bits are not guaranteed.
- The temporary archive is removed on normal exit or error. The destination is **never automatically deleted**. A failure after creation leaves an empty or partial directory for inspection; choose a different destination or handle that directory yourself before retrying.
- Finish builds and writes first. Keep the source and destination parent stable until return. Copying is not an atomic publication; consumers must wait for completion. Exclusive creation does not protect against concurrent junction swaps, overlapping parent/child copies or external writes.

There are no content hashes, manifests, source rechecks, automatic retries, cleanup workers, attribute/ACL changes or application-control operations in the copy operation.

## CLI JSON output and exit codes

During normal execution, the CLI writes one JSON object to stdout; diagnostics and subprocess commands go to stderr.

| Exit code | `status` | Meaning |
| --- | --- | --- |
| 0 | `copied` | Packing and extraction succeeded. |
| 0 | `dry_run` | Validation succeeded; nothing was copied. |
| 1 | `failed` | A runtime failure. |
| 2 | — | Invalid CLI arguments; diagnostic text instead of JSON. |

The result contains `status` and `elapsed_seconds`, plus `source_path` and `windows_path` once validated. Failures include `error`; `destination_retained: true` appears only when this invocation created a destination and then failed.

`copied` means successful subprocess exit codes, **not content or filename round-trip verification**. `--help` and `--version` print text instead of JSON. Forced termination can leave a partial destination and may not produce JSON; there is no crash recovery.

## Known Windows tar limitations

Native tests against Windows tar 3.8.9 identified these unresolved cases:

| Case | Observed behavior |
| --- | --- |
| Emoji filenames | Extraction fails with an unreadable-filename error. |
| Decomposed Unicode filenames (NFD) | Names can be normalized to NFC without a nonzero exit status. |
| Long destination paths | tar cannot change into some long `-C` paths, even with the tested extended-path prefix. |

These are tool compatibility gaps, not Windows filesystem limitations. The CLI does not rename, case-convert or normalize filenames. Avoid affected names and paths when exact preservation is required.

## Performance benchmarks

Local WSL2-to-Windows measurements on an Intel Core Ultra 7 265K, using Go 1.27.3 and Windows bsdtar 3.8.9. Sources were on WSL ext4; every run copied to a fresh directory on the Windows C: drive.

| Workload | File data | Median copy time | Runs |
| --- | ---: | ---: | ---: |
| One large file | 512 MiB | 1.16 s | 3 |
| 10,000 files, 4 KiB each | 39.1 MiB | 2.29 s | 3 |
| Real mini-program build: 8,309 files, 2,552 subdirectories | 45.3 MiB | 2.68 s | 5 |

Times include CLI startup, validation, packing and extraction, but exclude fixture generation, verification and cleanup. The real build's first copy took 3.34 s; subsequent copies took 2.63–2.73 s. Its dry-run validation took about 0.22 s.

These are illustrative results, not speed guarantees. Caches were not flushed, synthetic sources were freshly written, and `/tmp` was a memory-backed tmpfs used for tar staging. No forced disk flush was measured. File count, directory structure, storage and caching can materially change performance.

## FAQ

### Does wsl-copy synchronize files or overwrite an existing Windows folder?

No. It is a one-way, create-only directory copy tool, not a synchronization or mirror tool. Every existing destination is refused, including empty directories and dangling links. Choose a new target path for each copy; there is no merge, overwrite or automatic destination cleanup.

### Can I copy a single file or copy from Windows to WSL?

No. The source must be a directory on the WSL filesystem, and the destination must be a new Windows directory. Individual-file and Windows-to-WSL copying are outside the supported scope.

### Do I need Go or Python to run the CLI?

No. Go is needed to build the binary, not to run it. The Linux executable runs inside WSL and uses GNU tar, `wslpath`, Windows tar and PowerShell. See [Windows tool paths](#windows-tool-paths) if those executables are not on your PATH.

## Development and tests

```bash
go test ./...
go vet ./...
go fix -diff ./...
```

Follow the [Modern Go Guidelines](https://github.com/JetBrains/go-modern-guidelines) for the version declared in `go.mod`, without changing existing copy or JSON contracts. `go fix -diff` is read-only and exits nonzero if modernization suggestions remain.

Tests are organized by contract, not repeated across every layer:

- `wslcopy/copy_test.go` covers safety, dry-run validation, caller cancellation and exclusive destination creation.
- `wslcopy/command_test.go` exercises transfers through the public `Copy` API: content, empty directories, packing/extraction failures, ownership and archive cleanup. `paths_test.go` holds path and filename validation tables.
- `cmd/wsl-copy/main_test.go` covers arguments, help and version output. `wslcopy/cli_test.go` builds the real CLI and checks only its JSON, diagnostics and exit codes from outside the checkout, including omitted fields and invalid-UTF-8 replacement.
- `wslcopy/integration_test.go` covers native Windows path checks, junctions, tar compatibility and retention of an actual Windows destination after failure.

Shared fixtures live in `wslcopy/fixtures_test.go`. Tree assertions compare exact entry sets, types and contents; the local tar shim forwards extraction arguments after translating the destination path. Local command fixtures and GNU tar substitutions are not proof of Windows tar compatibility. Add tests for distinct contracts or reproduced regressions, rather than duplicating filesystem assertions in the CLI layer.

Run native tests explicitly, setting the tool-path environment variables above if needed:

```bash
make test-windows
# Equivalent: WSL_COPY_WINDOWS_TESTS=1 go test -count=1 -run '^TestWindows$' -v ./wslcopy
```

Native tests use only randomly named fixtures in the current Windows user's temporary directory. Compatibility skips require the recognized tar version and specific observed failure; preparation errors and unrelated failures still fail the test. A runtime that preserves the tested names and paths passes those tests normally.

Keep copying and safety policy in the public `wslcopy/` package, with tests beside the code they exercise. Keep argument parsing, environment defaults, signals, timing, JSON and exit codes in `cmd/wsl-copy`. Use pflag for CLI argument parsing and muonsoft/validation for filename checks, and prefer the standard library and system tools elsewhere. Keep archive transfer separate from destination policy; do not add deletion permissions, parallel extraction, archive splitting, services or workflow frameworks. New copy modes require an explicit safety contract. Write documentation, help and comments in English; multilingual test filenames are intentional. Development automation must not install dependencies or commands, change global configuration, or publish releases without explicit approval.

## Automated builds and releases

[Build and release workflow](https://github.com/wangnan0916/wsl-copy/actions/workflows/release.yml) runs on `main` pushes, pull requests and manual dispatches. It installs the Go version from `go.mod`, checks formatting, runs Linux tests with the race detector, vets code, verifies dependencies, checks modernization, and packages Linux amd64/arm64 archives with SHA256SUMS. CI artifacts are retained for seven days.

Only **pushing a new version tag** publishes a GitHub Release. Branch/PR runs and manual dispatches build but do not publish. Actions are pinned to commit SHAs; the build job is read-only, and only the tag-gated publish job has release-write permission through the built-in `GITHUB_TOKEN`. No personal access token is needed.

After reviewing the commit and running native Windows tests locally, release a new version:

```bash
git tag -a v0.1.1 -m "Release v0.1.1"
git push origin v0.1.1
```

Tags must use `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE`. Tags starting with `v0.` and tags with a prerelease suffix are marked as GitHub prereleases. The workflow uploads the two archives and checksum file to a draft before publishing it. It never overwrites an existing release or its assets; if an upload fails, inspect the draft before any manual recovery. Do not move published tags or replace published assets.

Hosted Linux CI cannot replace WSL/Windows-native tests; ARM64 remains cross-build-only. The existing `v0.1.0` release was built locally. Automation applies to new tags containing the workflow; it does not rebuild or change that release.

### Local release builds

```bash
make release VERSION=v0.1.0
```

This builds `dist/wsl-copy-linux-amd64` and `dist/wsl-copy-linux-arm64` with CGO disabled, then packages each as `dist/wsl-copy-linux-<arch>.tar.gz` with `LICENSE` and `THIRD_PARTY_NOTICES`. `dist/SHA256SUMS` covers the two release archives. It does not install commands or publish anything.

Publish only the two archives and `SHA256SUMS` as release assets, not the entire `dist/` directory: local benchmark reports and packaging intermediates are not distribution files. Build releases from a clean, tagged commit and state which architectures were actually tested.

## License

[MIT](LICENSE). Bundled dependencies and the Go runtime retain their respective licenses; see [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
