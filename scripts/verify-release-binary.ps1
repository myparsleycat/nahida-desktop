param(
    [Parameter(Mandatory = $true)][string]$Executable,
    [Parameter(Mandatory = $true)][string]$AppVersion
)

$ErrorActionPreference = 'Stop'

$item = Get-Item -LiteralPath $Executable
$actual = $item.VersionInfo.ProductVersion
if ($actual -ne $AppVersion) {
    throw "ProductVersion mismatch: expected '$AppVersion', got '$actual'"
}

$bytes = [IO.File]::ReadAllBytes($item.FullName)
if (-not [Text.Encoding]::ASCII.GetString($bytes).Contains($AppVersion)) {
    throw "Linked AppVersion '$AppVersion' was not found in $Executable"
}

Write-Host "Verified ProductVersion and linked AppVersion: $AppVersion"
