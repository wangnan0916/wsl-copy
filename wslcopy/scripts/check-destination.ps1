# The Go launcher supplies $parents and $p as literal extended-length Windows paths.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
foreach ($parent in $parents) {
    $a = [IO.File]::GetAttributes($parent)
    if ($a -band ([IO.FileAttributes]::ReparsePoint -bor [IO.FileAttributes]::Device)) {
        throw "Refusing a reparse point/device: $parent"
    }
    if (-not ($a -band [IO.FileAttributes]::Directory)) {
        throw "Expected an existing parent directory: $parent"
    }
}
$attributes = $null
try { $attributes = [IO.File]::GetAttributes($p) }
catch [IO.FileNotFoundException] {}
catch [IO.DirectoryNotFoundException] {}
if ($null -ne $attributes) { throw "Destination already exists: $p" }
# A handled FileNotFoundException must not become the process exit status.
exit 0
