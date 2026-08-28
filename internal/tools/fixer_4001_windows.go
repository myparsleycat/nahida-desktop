//go:build windows

package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

func executeD3DBuild(ctx context.Context, vcvarsPath, projectPath string) error {
	cmd := cmdScript(ctx, d3dBuildCommand(vcvarsPath, projectPath))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w\n%s", err, tailBuildOutput(output.String(), 120))
	}
	return nil
}

func d3dBuildCommand(vcvarsPath, projectPath string) string {
	return fmt.Sprintf(`"%s" && cd /d "%s" && msbuild StereovisionHacks.sln /nologo /verbosity:minimal /consoleloggerparameters:ErrorsOnly /p:Configuration=Release /p:Platform=x64`, vcvarsPath, projectPath)
}

// cmdScript runs a cmd.exe script without Go's Windows argv quoting.
// exec.Command would turn inner quotes into \", which cmd.exe then treats as
// part of the command name (the original Electron exec() path does not).
func cmdScript(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    `cmd.exe /d /s /c "` + script + `"`,
	}
	return cmd
}

func elevatedCopyFiles(copies []fileCopy) error {
	var entries strings.Builder
	for _, item := range copies {
		fmt.Fprintf(&entries, "[pscustomobject]@{ SourcePath = %s; TargetPath = %s }\n", psLiteral(item.Source), psLiteral(item.Target))
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$Copies = @(
%s)
foreach ($Copy in $Copies) {
  [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($Copy.TargetPath)) | Out-Null
  Copy-Item -LiteralPath $Copy.SourcePath -Destination $Copy.TargetPath -Force
}`, entries.String())
	return runElevatedPowerShell(script, "XXMI_ERR_ELEVATED_COPY_FAILED")
}

func elevatedRemoveFiles(paths []string) error {
	var entries strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&entries, "%s\n", psLiteral(path))
	}
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$Paths = @(
%s)
foreach ($PathValue in $Paths) {
  if (Test-Path -LiteralPath $PathValue) { Remove-Item -LiteralPath $PathValue -Force }
}`, entries.String())
	return runElevatedPowerShell(script, "XXMI_ERR_ELEVATED_REMOVE_FAILED")
}

func runElevatedPowerShell(script, errorPrefix string) error {
	inner := encodePowerShell(script)
	outer := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
try {
  $Process = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', '%s')
  if ($null -eq $Process -or $null -eq $Process.ExitCode) { exit 1 }
  exit $Process.ExitCode
} catch { exit 1 }`, inner)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(outer))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", errorPrefix, err)
	}
	return nil
}

func psLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func encodePowerShell(command string) string {
	units := utf16.Encode([]rune(command))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
