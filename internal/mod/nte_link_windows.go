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

// nteMoveScript defines the helpers both NTE scripts use to move a mods folder's content. Move-Item in
// Windows PowerShell 5.1 cannot move a directory to another volume, so a move across volumes goes through
// robocopy. Content is moved into a staging folder with a fixed name, which lets a failed or interrupted
// run, including the elevated retry, continue from where the previous one stopped.
const nteMoveScript = `
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

function Move-Contents([string]$From, [string]$To) {
  [System.IO.Directory]::CreateDirectory($To) | Out-Null
  $FromRoot = [System.IO.Path]::GetPathRoot((Normalize-PathValue $From))
  $ToRoot = [System.IO.Path]::GetPathRoot((Normalize-PathValue $To))
  if ($FromRoot.Equals($ToRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    try {
      Get-ChildItem -LiteralPath $From -Force | Move-Item -Destination $To -Force
    } catch { exit 36 }
    return
  }
  & robocopy.exe $From $To /E /MOVE /SJ /SL /COPY:DAT /DCOPY:DAT /R:1 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
  if ($LASTEXITCODE -ge 8) { exit 36 }
}
`

func reconcileNteJunction(targetPath, linkPath string) error {
	return runNteMutation(nteLinkScript(targetPath, linkPath), "NTE_MODS_LINK_ELEVATION_FAILED", map[int]string{
		32: "NTE_MODS_LINK_CONFLICT",
		33: "NTE_MODS_LINK_PATH_OCCUPIED",
		34: "NTE_MODS_LINK_JUNCTION_FAILED",
		35: "NTE_MODS_UNLINK_JUNCTION_FAILED",
		36: "NTE_MODS_MOVE_FAILED",
	})
}

// nteLinkScript moves the content of an existing game mods folder at linkPath into targetPath and replaces
// the folder with a junction to targetPath.
func nteLinkScript(targetPath, linkPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$TargetPath = %s
$LinkPath = %s
$Staging = $TargetPath + '.nte-link'
%s
[System.IO.Directory]::CreateDirectory($TargetPath) | Out-Null
[System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($LinkPath)) | Out-Null
$Resuming = Test-Path -LiteralPath $Staging

if (Test-Path -LiteralPath $LinkPath) {
  $Item = Get-Item -LiteralPath $LinkPath -Force
  if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    $ExistingTarget = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
    if (Same-Path $ExistingTarget $TargetPath) {
      if (-not $Resuming) { exit 0 }
      # The junction already serves the library, so leftover staging may only fill an empty library.
      if (Has-Any-FileSystemContent $TargetPath) { exit 32 }
      Remove-Item -LiteralPath $TargetPath -Recurse -Force
      [System.IO.Directory]::Move($Staging, $TargetPath)
      exit 0
    }
    $RmdirCommand = 'rmdir "' + $LinkPath + '"'
    cmd.exe /d /c $RmdirCommand | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 35 }
  } elseif (-not $Item.PSIsContainer) {
    exit 33
  } elseif (Has-Any-FileSystemContent $LinkPath) {
    if (Has-Any-FileSystemContent $TargetPath) { exit 32 }
    Move-Contents $LinkPath $Staging
    if ((Test-Path -LiteralPath $LinkPath) -and (Has-Any-FileSystemContent $LinkPath)) { exit 36 }
    if (Test-Path -LiteralPath $LinkPath) { Remove-Item -LiteralPath $LinkPath -Recurse -Force }
  } else {
    Remove-Item -LiteralPath $LinkPath -Recurse -Force
  }
}

if (Test-Path -LiteralPath $Staging) {
  if (Has-Any-FileSystemContent $TargetPath) {
    # The game folder was already emptied into staging, so the conflict hands its content back.
    if (-not (Test-Path -LiteralPath $LinkPath)) {
      Move-Contents $Staging $LinkPath
      # robocopy removes the staging folder itself after moving across volumes.
      if (Test-Path -LiteralPath $Staging) {
        if (Has-Any-FileSystemContent $Staging) { exit 36 }
        Remove-Item -LiteralPath $Staging -Recurse -Force
      }
    }
    exit 32
  }
  Remove-Item -LiteralPath $TargetPath -Recurse -Force
  [System.IO.Directory]::Move($Staging, $TargetPath)
}

$MklinkCommand = 'mklink /J "' + $LinkPath + '" "' + $TargetPath + '"'
cmd.exe /d /c $MklinkCommand | Out-Null
if ($LASTEXITCODE -ne 0) { exit 34 }
`, ntePSLiteral(targetPath), ntePSLiteral(linkPath), nteMoveScript)
}

func unlinkNteModsFolder(linkPath string) error {
	return runNteMutation(nteUnlinkScript(linkPath), "NTE_MODS_UNLINK_ELEVATION_FAILED", map[int]string{
		35: "NTE_MODS_UNLINK_JUNCTION_FAILED",
		36: "NTE_MODS_MOVE_FAILED",
	})
}

// nteUnlinkScript replaces the junction at linkPath with a real folder holding the content of its target.
// The content is gathered next to the junction before the junction is removed, so a failed move leaves the
// junction in place instead of an empty folder.
func nteUnlinkScript(linkPath string) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ModFolderPath = %s
$Staging = $ModFolderPath + '.nte-unlink'
%s
if (-not (Test-Path -LiteralPath $ModFolderPath) -and -not (Test-Path -LiteralPath $Staging)) { exit 0 }

if (Test-Path -LiteralPath $ModFolderPath) {
  $Item = Get-Item -LiteralPath $ModFolderPath -Force
  if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) {
    if (-not (Test-Path -LiteralPath $Staging)) { exit 0 }
    if (Has-Any-FileSystemContent $ModFolderPath) { exit 36 }
    Remove-Item -LiteralPath $ModFolderPath -Recurse -Force
  } else {
    $TargetPath = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
    if ((Test-Path -LiteralPath $TargetPath) -and (Has-Any-FileSystemContent $TargetPath)) {
      Move-Contents $TargetPath $Staging
      [System.IO.Directory]::CreateDirectory($TargetPath) | Out-Null
      if (Has-Any-FileSystemContent $TargetPath) { exit 36 }
    }
    $RmdirCommand = 'rmdir "' + $ModFolderPath + '"'
    cmd.exe /d /c $RmdirCommand | Out-Null
    if ($LASTEXITCODE -ne 0) { exit 35 }
  }
}

if (Test-Path -LiteralPath $Staging) {
  [System.IO.Directory]::Move($Staging, $ModFolderPath)
} else {
  [System.IO.Directory]::CreateDirectory($ModFolderPath) | Out-Null
}
`, ntePSLiteral(linkPath), nteMoveScript)
}

func runNteMutation(script, elevationError string, contractErrors map[int]string) error {
	code, err := executeNtePowerShell(script, false)
	if err == nil {
		return nil
	}
	if message, ok := contractErrors[code]; ok {
		return infra.AnnotateError(
			infra.WithCause(errors.New(message), err),
			infra.Diagnostic{
				Operation: "nte-mutation",
				Stage:     "execute",
				Fields:    map[string]any{"exitCode": code, "elevated": false},
			},
		)
	}
	initialErr := err
	code, err = executeNtePowerShell(script, true)
	if err == nil {
		return nil
	}
	if message, ok := contractErrors[code]; ok {
		return infra.AnnotateError(
			infra.WithCause(errors.New(message), errors.Join(initialErr, err)),
			infra.Diagnostic{
				Operation: "nte-mutation",
				Stage:     "execute",
				Fields:    map[string]any{"exitCode": code, "elevated": true},
			},
		)
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
		command = exec.Command(
			"powershell.exe",
			"-NoProfile",
			"-ExecutionPolicy",
			"Bypass",
			"-EncodedCommand",
			encodeNtePowerShell(outer),
		)
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
