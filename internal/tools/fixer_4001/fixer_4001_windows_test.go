//go:build windows

package fixer4001

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/elevated"
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
	msbuild := "@echo off\r\necho ran> msbuild-ran.txt\r\ncall git\r\n"
	if err := os.WriteFile(filepath.Join(binDir, "msbuild.cmd"), []byte(msbuild), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The portable Git directory is not on PATH; the build must put it first so the version generator finds it.
	gitDir := filepath.Join(root, "portable git", "cmd")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	git := "@echo off\r\necho ran> git-ran.txt\r\n"
	if err := os.WriteFile(filepath.Join(gitDir, "git.cmd"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := executeD3DBuild(
		context.Background(),
		vcvarsPath,
		projectDir,
		filepath.Join(gitDir, "git.exe"),
	); err != nil {
		t.Fatalf("executeD3DBuild() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "msbuild-ran.txt")); err != nil {
		t.Fatalf("msbuild did not run after vcvars failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "git-ran.txt")); err != nil {
		t.Fatalf("build did not resolve git from the supplied Git directory: %v", err)
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

func TestElevatedFileOperationsKeepErrorCodes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "built.dll")
	if err := os.WriteFile(source, []byte("built"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "importer", targetD3D11DLL)
	stale := filepath.Join(root, "importer", diversifierBackupPre+"1234567-1.bak")

	helper := &recordingElevatedFiles{}
	lease := elevated.NewFileLease(helper)
	if err := elevatedCopyFiles(t.Context(), lease, []fileCopy{{Source: source, Target: target}}); err != nil {
		t.Fatal(err)
	}
	if err := elevatedRemoveFiles(t.Context(), lease, []string{stale}); err != nil {
		t.Fatal(err)
	}
	if got, want := helper.summary(), []string{"copy " + target, "remove " + stale}; !slices.Equal(got, want) {
		t.Fatalf("helper operations = %q, want %q", got, want)
	}
	if helper.content[target] != "built" || helper.acquires != 1 {
		t.Fatalf("staged content = %q, acquires = %d", helper.content, helper.acquires)
	}

	refused := errors.New("target is not a regular file")
	failing := &recordingElevatedFiles{applyErr: refused}
	lease = elevated.NewFileLease(failing)
	err := elevatedCopyFiles(t.Context(), lease, []fileCopy{{Source: source, Target: target}})
	if !errors.Is(err, refused) || !strings.HasPrefix(err.Error(), "XXMI_ERR_ELEVATED_COPY_FAILED: ") {
		t.Fatalf("copy error = %v", err)
	}
	err = elevatedRemoveFiles(t.Context(), lease, []string{stale})
	if !errors.Is(err, refused) || !strings.HasPrefix(err.Error(), "XXMI_ERR_ELEVATED_REMOVE_FAILED: ") {
		t.Fatalf("remove error = %v", err)
	}

	// A source that cannot be staged fails before the user is asked for consent.
	unasked := &recordingElevatedFiles{}
	err = elevatedCopyFiles(
		t.Context(),
		elevated.NewFileLease(unasked),
		[]fileCopy{{Source: filepath.Join(root, "missing.dll"), Target: target}},
	)
	if !errors.Is(err, os.ErrNotExist) || !strings.HasPrefix(err.Error(), "XXMI_ERR_ELEVATED_COPY_FAILED: ") {
		t.Fatalf("missing source error = %v", err)
	}
	if unasked.acquires != 0 {
		t.Fatal("an unreadable source started the helper")
	}
}
