//go:build windows

package mod

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"

	"nahida.live/desktop/internal/infra"
)

func reconcileNteJunction(targetPath, linkPath string) error {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$TargetPath = %s
$LinkPath = %s

function Normalize-PathValue([string]$PathValue) {
  return [System.IO.Path]::GetFullPath($PathValue).TrimEnd([char[]]@([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar))
}

function Same-Path([string]$Left, [string]$Right) {
  return (Normalize-PathValue $Left).Equals((Normalize-PathValue $Right), [System.StringComparison]::OrdinalIgnoreCase)
}

function Has-Any-FileSystemContent([string]$PathValue) {
  foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force -ErrorAction SilentlyContinue) {
    if (-not $Child.PSIsContainer) { return $true }
    if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return $true }
    if (Has-Any-FileSystemContent $Child.FullName) { return $true }
  }
  return $false
}

[System.IO.Directory]::CreateDirectory($TargetPath) | Out-Null
[System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($LinkPath)) | Out-Null

if (Test-Path -LiteralPath $LinkPath) {
  $Item = Get-Item -LiteralPath $LinkPath -Force
  if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    $ExistingTarget = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
    if (Same-Path $ExistingTarget $TargetPath) { exit 0 }
    $RmdirCommand = 'rmdir "' + $LinkPath + '"'
    cmd.exe /d /c $RmdirCommand | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 35 }
  } elseif (-not $Item.PSIsContainer) {
    exit 33
  } elseif (-not (Has-Any-FileSystemContent $LinkPath)) {
    Remove-Item -LiteralPath $LinkPath -Force
  } elseif (Has-Any-FileSystemContent $TargetPath) {
    exit 32
  } else {
    Get-ChildItem -LiteralPath $TargetPath -Force | Remove-Item -Recurse -Force
    Get-ChildItem -LiteralPath $LinkPath -Force | Move-Item -Destination $TargetPath -Force
    Remove-Item -LiteralPath $LinkPath -Force
  }
}

$MklinkCommand = 'mklink /J "' + $LinkPath + '" "' + $TargetPath + '"'
cmd.exe /d /c $MklinkCommand | Out-Null
if ($LASTEXITCODE -ne 0) { exit 34 }
`, ntePSLiteral(targetPath), ntePSLiteral(linkPath))
	return runNteMutation(script, "NTE_MODS_LINK_ELEVATION_FAILED", map[int]string{
		32: "NTE_MODS_LINK_CONFLICT",
		33: "NTE_MODS_LINK_PATH_OCCUPIED",
		34: "NTE_MODS_LINK_JUNCTION_FAILED",
		35: "NTE_MODS_UNLINK_JUNCTION_FAILED",
	})
}

func unlinkNteModsFolder(linkPath string) error {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ModFolderPath = %s

function Has-Any-FileSystemContent([string]$PathValue) {
  foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force -ErrorAction SilentlyContinue) {
    if (-not $Child.PSIsContainer) { return $true }
    if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return $true }
    if (Has-Any-FileSystemContent $Child.FullName) { return $true }
  }
  return $false
}

if (-not (Test-Path -LiteralPath $ModFolderPath)) { exit 0 }

$Item = Get-Item -LiteralPath $ModFolderPath -Force
if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) { exit 0 }

$TargetPath = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
$ShouldMoveTargetEntries = (Test-Path -LiteralPath $TargetPath) -and (Has-Any-FileSystemContent $TargetPath)

$RmdirCommand = 'rmdir "' + $ModFolderPath + '"'
cmd.exe /d /c $RmdirCommand | Out-Null
if ($LASTEXITCODE -ne 0) { exit 35 }

[System.IO.Directory]::CreateDirectory($ModFolderPath) | Out-Null

if ($ShouldMoveTargetEntries) {
  Get-ChildItem -LiteralPath $TargetPath -Force | Move-Item -Destination $ModFolderPath -Force
}
`, ntePSLiteral(linkPath))
	return runNteMutation(script, "NTE_MODS_UNLINK_ELEVATION_FAILED", map[int]string{
		35: "NTE_MODS_UNLINK_JUNCTION_FAILED",
	})
}

func runNteMutation(script, elevationError string, contractErrors map[int]string) error {
	code, err := executeNtePowerShell(script, false)
	if err == nil {
		return nil
	}
	if message, ok := contractErrors[code]; ok {
		return infra.AnnotateError(infra.WithCause(errors.New(message), err), infra.Diagnostic{Operation: "nte-mutation", Stage: "execute", Fields: map[string]any{"exitCode": code, "elevated": false}})
	}
	initialErr := err
	code, err = executeNtePowerShell(script, true)
	if err == nil {
		return nil
	}
	if message, ok := contractErrors[code]; ok {
		return infra.AnnotateError(infra.WithCause(errors.New(message), errors.Join(initialErr, err)), infra.Diagnostic{Operation: "nte-mutation", Stage: "execute", Fields: map[string]any{"exitCode": code, "elevated": true}})
	}
	return infra.WithCause(fmt.Errorf("%s: %w", elevationError, err), initialErr)
}

func executeNtePowerShell(script string, elevated bool) (int, error) {
	encoded := encodeNtePowerShell(script)
	command := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
	if elevated {
		outer := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
try {
  $Process = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', '%s')
  if ($null -eq $Process -or $null -eq $Process.ExitCode) { exit 1 }
  exit $Process.ExitCode
} catch { exit 1 }`, encoded)
		command = exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeNtePowerShell(outer))
	}
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), err
		}
		return -1, err
	}
	return 0, nil
}

func ntePSLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func encodeNtePowerShell(command string) string {
	units := utf16.Encode([]rune(command))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(data)
}
