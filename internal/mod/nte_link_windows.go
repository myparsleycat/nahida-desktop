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

# An entry that cannot be listed fails the script rather than counting as empty, because an empty folder may be
# deleted.
function Has-Any-FileSystemContent([string]$PathValue) {
  foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force) {
    if (-not $Child.PSIsContainer) { return $true }
    if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return $true }
    if (Has-Any-FileSystemContent $Child.FullName) { return $true }
  }
  return $false
}

# Remove-EmptyTree deletes a folder that holds only empty folders. A file fails the delete, so content is never
# removed even if it appeared after the emptiness check.
function Remove-EmptyTree([string]$PathValue) {
  foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force -Directory) {
    if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) { Remove-EmptyTree $Child.FullName }
    else { [System.IO.Directory]::Delete($Child.FullName, $false) }
  }
  $Folder = [System.IO.DirectoryInfo]::new($PathValue)
  $Folder.Attributes = $Folder.Attributes -band (-bnot [System.IO.FileAttributes]::ReadOnly)
  $Folder.Delete($false)
}

function Move-Contents([string]$From, [string]$To) {
  [System.IO.Directory]::CreateDirectory($To) | Out-Null
  $FromRoot = [System.IO.Path]::GetPathRoot((Normalize-PathValue $From))
  $ToRoot = [System.IO.Path]::GetPathRoot((Normalize-PathValue $To))
  if ($FromRoot.Equals($ToRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    try {
      Get-ChildItem -LiteralPath $From -Force | Move-Item -Destination $To -Force
      return
    } catch {
      # Move-Item cannot merge into a folder an earlier run already staged, nor cross a volume mounted below
      # the same drive letter. robocopy handles both.
    }
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
      Remove-EmptyTree $TargetPath
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
    if (Test-Path -LiteralPath $LinkPath) { Remove-EmptyTree $LinkPath }
  } else {
    Remove-EmptyTree $LinkPath
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
        Remove-EmptyTree $Staging
      }
    }
    exit 32
  }
  Remove-EmptyTree $TargetPath
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
    Remove-EmptyTree $ModFolderPath
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

// nteMoveFailedCode is the one contract code an elevated run can resolve: a move denied by the folder's
// permissions. The script resumes from its staging folder, so the elevated run continues the move.
const nteMoveFailedCode = 36

func runNteMutation(script, elevationError string, contractErrors map[int]string) error {
	code, err := executeNtePowerShell(script, false)
	if err == nil {
		return nil
	}
	if message, ok := contractErrors[code]; ok && code != nteMoveFailedCode {
		return infra.AnnotateError(
			infra.WithCause(errors.New(message), err),
			infra.Diagnostic{
				Operation: "nte-mutation",
				Stage:     "execute",
				Fields:    map[string]any{"exitCode": code, "elevated": false},
			},
		)
	}
	initialCode, initialErr := code, err
	code, err = executeNtePowerShell(script, true)
	if err == nil {
		return nil
	}
	elevated := true
	message, ok := contractErrors[code]
	if !ok {
		// A declined or failed elevation keeps the move failure the user can act on.
		message, ok = contractErrors[initialCode]
		code, elevated = initialCode, false
	}
	if ok {
		return infra.AnnotateError(
			infra.WithCause(errors.New(message), errors.Join(initialErr, err)),
			infra.Diagnostic{
				Operation: "nte-mutation",
				Stage:     "execute",
				Fields:    map[string]any{"exitCode": code, "elevated": elevated},
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
