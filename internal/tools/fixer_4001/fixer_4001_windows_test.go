//go:build windows

package fixer4001

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestCmdScriptRunsQuotedBatchWithSpaces(t *testing.T) {
	root := t.TempDir()
	batDir := filepath.Join(root, "Program Files (x86)", "Build Tools")
	projectDir := filepath.Join(root, "XXMI Libs Package")
	if err := os.MkdirAll(batDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	batPath := filepath.Join(batDir, "vcvars64.bat")
	markerPath := filepath.Join(projectDir, "ran.txt")
	script := "@echo off\r\ncd /d \"%~dp0\"\r\necho ran> \"" + markerPath + "\"\r\n"
	if err := os.WriteFile(batPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := cmdScript(context.Background(), `"`+batPath+`" && cd /d "`+projectDir+`" && echo ok`)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		t.Fatalf("cmdScript failed: %v\n%s", err, output.String())
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("batch did not run: %v\n%s", err, output.String())
	}
	if strings.Contains(output.String(), `\"`) {
		t.Fatalf("cmd.exe received escaped quotes: %s", output.String())
	}
}

func TestD3DBuildCommandQuotesVSAndProjectPaths(t *testing.T) {
	got, err := d3dBuildCommand(`C:\Program Files (x86)\vcvars64.bat`, `C:\Temp\XXMI Libs`)
	if err != nil {
		t.Fatal(err)
	}
	want := `"C:\Program Files (x86)\vcvars64.bat" & cd /d "C:\Temp\XXMI Libs" && msbuild StereovisionHacks.sln /nologo /verbosity:minimal /p:Configuration=Release /p:Platform=x64`
	if got != want {
		t.Fatalf("d3dBuildCommand() = %q, want %q", got, want)
	}
}

func TestExecuteD3DBuildIgnoresVCVarsExitCode(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "XXMI Libs")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	vcvarsPath := filepath.Join(root, "vcvars64.bat")
	if err := os.WriteFile(vcvarsPath, []byte("@echo off\r\nexit /b 1\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	msbuild := "@echo off\r\necho ran> msbuild-ran.txt\r\n"
	if err := os.WriteFile(filepath.Join(binDir, "msbuild.cmd"), []byte(msbuild), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := executeD3DBuild(context.Background(), vcvarsPath, projectDir); err != nil {
		t.Fatalf("executeD3DBuild() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "msbuild-ran.txt")); err != nil {
		t.Fatalf("msbuild did not run after vcvars failed: %v", err)
	}
}

func TestD3DBuildCommandRejectsUnsafePaths(t *testing.T) {
	project := `C:\Temp\XXMI Libs`
	for _, vcvars := range []string{
		`C:\Program Files (x86)\vcvars64.bat" && calc && "`,
		`C:\Tools\%PATH%\vcvars64.bat`,
		`\\attacker\share\vcvars64.bat`,
	} {
		if _, err := d3dBuildCommand(vcvars, project); err == nil {
			t.Fatalf("d3dBuildCommand(%q) succeeded", vcvars)
		}
	}
}

func TestResolveVSDevCmdRejectsUNCAndDeviceNamespace(t *testing.T) {
	for _, path := range []string{
		`\\attacker\share\vcvars64.bat`,
		`//attacker/share/vcvars64.bat`,
		`\\?\C:\Windows\vcvars64.bat`,
		`\\.\C:\Windows\vcvars64.bat`,
	} {
		if got := resolveVSDevCmd(path); got != "" {
			t.Errorf("resolveVSDevCmd(%q) = %q", path, got)
		}
	}
}

func TestElevatedPowerShellEncodingAndLiteralEscaping(t *testing.T) {
	t.Parallel()
	command := "Write-Output '한글 🚀'"
	encoded := encodePowerShell(command)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(data)%2 != 0 {
		t.Fatalf("encoded UTF-16 byte length = %d", len(data))
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	if decoded := string(utf16.Decode(units)); decoded != command {
		t.Fatalf("decoded command = %q, want %q", decoded, command)
	}
	if got, want := psLiteral(`C:\O'Brien\file.dll`), `'C:\O''Brien\file.dll'`; got != want {
		t.Fatalf("psLiteral = %q, want %q", got, want)
	}
}
