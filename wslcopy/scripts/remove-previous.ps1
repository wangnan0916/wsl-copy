# Only an invocation-owned sibling container may reach this operation.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
if ([IO.Path]::GetFileName($p) -notmatch '^\.wsl-copy-[0-9]+$') {
    throw "Refusing an unrecognized retirement directory: $p"
}
foreach ($parent in $parents) {
    if (-not ((Get-SafeAttributes $parent) -band [IO.FileAttributes]::Directory)) {
        throw "Expected an existing parent directory: $parent"
    }
}
$paths = @(Get-SafeTree $p)
# Validate the entire tree first, then recheck each entry before a nonrecursive delete.
for ($i = $paths.Count - 1; $i -ge 0; $i--) {
    $entry = $paths[$i]
    $a = Get-SafeAttributes $entry
    if ($a -band [IO.FileAttributes]::Directory) {
        [IO.Directory]::Delete($entry, $false)
    } else {
        [IO.File]::Delete($entry)
    }
}
exit 0
