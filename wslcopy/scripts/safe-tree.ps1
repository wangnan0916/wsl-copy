function Get-SafeAttributes($path) {
    $a = [IO.File]::GetAttributes($path)
    if ($a -band ([IO.FileAttributes]::ReparsePoint -bor [IO.FileAttributes]::Device)) {
        throw "Refusing a reparse point/device: $path"
    }
    return $a
}

# Return parent-before-child paths without ever descending into a reparse point.
function Get-SafeTree($root) {
    $paths = [Collections.Generic.List[string]]::new()
    $pending = [Collections.Generic.Stack[string]]::new()
    if (-not ((Get-SafeAttributes $root) -band [IO.FileAttributes]::Directory)) {
        throw "Expected a directory: $root"
    }
    $paths.Add($root)
    $pending.Push($root)
    while ($pending.Count) {
        foreach ($entry in [IO.Directory]::EnumerateFileSystemEntries($pending.Pop())) {
            $a = Get-SafeAttributes $entry
            $paths.Add($entry)
            if ($a -band [IO.FileAttributes]::Directory) { $pending.Push($entry) }
        }
    }
    return $paths.ToArray()
}
