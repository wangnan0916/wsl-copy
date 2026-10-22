# One detached process receives only its invocation-owned retirement container.
$ErrorActionPreference = 'Stop'
$log = [IO.Path]::Combine($p, 'cleanup.log')
try {
    if ([IO.Path]::GetFileName($p) -notmatch '^\.wsl-copy-[0-9]+$') {
        throw "Refusing an unrecognized retirement directory: $p"
    }
    foreach ($parent in @($parents) + @($p)) {
        if (-not ((Get-SafeAttributes $parent) -band [IO.FileAttributes]::Directory)) {
            throw "Expected an existing parent directory: $parent"
        }
    }
    if ((Get-SafeAttributes $log) -band [IO.FileAttributes]::Directory) { throw 'Expected a regular cleanup log' }
    foreach ($entry in [IO.Directory]::EnumerateFileSystemEntries($p)) {
        if ([IO.Path]::GetFileName($entry) -cnotin @('previous', 'cleanup.log')) {
            throw "Unexpected entry in retirement container: $entry"
        }
    }
    [IO.File]::AppendAllText($log, "running`r`n")
    $paths = @(Get-SafeTree $p)
    # Validate the entire old tree before deletion, then recheck every entry.
    # Keep the log until success, and never follow links or recurse during deletion.
    for ($i = $paths.Count - 1; $i -ge 0; $i--) {
        $entry = $paths[$i]
        if ($entry -eq $p -or $entry -eq $log) { continue }
        $a = Get-SafeAttributes $entry
        if ($a -band [IO.FileAttributes]::Directory) {
            [IO.Directory]::Delete($entry, $false)
        } else {
            [IO.File]::Delete($entry)
        }
    }
    [IO.File]::Delete($log)
    [IO.Directory]::Delete($p, $false)
} catch {
    $failure = $_.ToString()
    try {
        # Logging is best-effort and must not follow a substituted parent or log.
        foreach ($parent in @($parents) + @($p)) {
            if (-not ((Get-SafeAttributes $parent) -band [IO.FileAttributes]::Directory)) { throw 'Unsafe log parent' }
        }
        # Final container deletion can fail after its log was removed. Recreate
        # only a missing log, exclusively; validate any existing entry before use.
        try { [IO.File]::Open($log, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read).Dispose() } catch [IO.IOException] {}
        if ((Get-SafeAttributes $log) -band [IO.FileAttributes]::Directory) { throw 'Unsafe cleanup log' }
        [IO.File]::AppendAllText($log, "failed: $failure`r`n")
    } catch {}
    exit 1
}
exit 0
