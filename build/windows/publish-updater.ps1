param(
    [Parameter(Mandatory = $true)][string]$BinDir,
    [Parameter(Mandatory = $true)][string]$AppName,
    [Parameter(Mandatory = $true)][string]$Arch
)

$ErrorActionPreference = 'Stop'

$src = Join-Path $BinDir "$AppName.exe"
$assetName = "$AppName-windows-$Arch.exe"
$dst = Join-Path $BinDir $assetName
Copy-Item -LiteralPath $src -Destination $dst -Force

$names = @($assetName)
$installerName = "$AppName-windows-$Arch-installer.exe"
$installer = Join-Path $BinDir $installerName
if (-not (Test-Path -LiteralPath $installer)) {
    throw "Missing installer release asset: $installer"
}
$names += $installerName

# Hash with .NET instead of Get-FileHash. Task launches this script through
# Git Bash, and that environment often fails to load PowerShell utility cmdlets.
function Get-Sha256Lower([string]$Path) {
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $stream = [System.IO.File]::OpenRead($Path)
        try {
            return [BitConverter]::ToString($sha.ComputeHash($stream)).Replace('-', '').ToLowerInvariant()
        }
        finally {
            $stream.Dispose()
        }
    }
    finally {
        $sha.Dispose()
    }
}

$lines = foreach ($name in $names) {
    $hash = Get-Sha256Lower (Join-Path $BinDir $name)
    "${hash}  ${name}"
}
[System.IO.File]::WriteAllLines((Join-Path $BinDir 'SHA256SUMS'), $lines, [System.Text.UTF8Encoding]::new($false))
