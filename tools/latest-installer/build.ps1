$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$projectRoot = Split-Path -Parent $root
$outputPath = Join-Path $projectRoot "dist\Nahida Desktop Latest Installer.exe"
$outputDir = Split-Path -Parent $outputPath
$sysoPath = Join-Path $PSScriptRoot "latest-installer.syso"

try {
    Push-Location $PSScriptRoot

    & windres -i "latest-installer.rc" -O coff -o "latest-installer.syso"
    if ($LASTEXITCODE -ne 0) {
        throw "windres failed with exit code $LASTEXITCODE."
    }

    New-Item -ItemType Directory -Path $outputDir -Force | Out-Null

    & go build -trimpath -ldflags="-H windowsgui -s -w" -o $outputPath .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
    Remove-Item -LiteralPath $sysoPath -ErrorAction SilentlyContinue
}
