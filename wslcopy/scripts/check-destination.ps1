# The launcher supplies literal paths and the shared Get-SafeAttributes/Get-SafeTree helpers.
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
foreach ($parent in $parents) {
    try { $a = Get-SafeAttributes $parent }
    catch [IO.FileNotFoundException] {
        if ($mappingRoot -and $parent.StartsWith($mappingRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { continue }
        throw
    }
    catch [IO.DirectoryNotFoundException] {
        if ($mappingRoot -and $parent.StartsWith($mappingRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { continue }
        throw
    }
    if (-not ($a -band [IO.FileAttributes]::Directory)) {
        throw "Expected an existing parent directory: $parent"
    }
}
$attributes = $null
try { $attributes = [IO.File]::GetAttributes($p) }
catch [IO.FileNotFoundException] {}
catch [IO.DirectoryNotFoundException] {}
if ($null -ne $attributes) {
    if (-not $replace) { throw "Destination already exists: $p" }
    $null = Get-SafeTree $p
}
# A handled FileNotFoundException must not become the process exit status.
exit 0
