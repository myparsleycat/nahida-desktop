package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxRejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	paths := []string{`..\outside.ini`, `C:\Windows\win.ini`, `file.ini:stream`, `\\?\C:\Windows`}
	for _, path := range paths {
		if _, err := sandbox.ReadFile("root", path, 0, 0); err == nil {
			t.Errorf("ReadFile(%q) unexpectedly succeeded", path)
		}
	}
}

func TestSandboxPreservesTextFormat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	utf8BOM := []byte{0xef, 0xbb, 0xbf}
	utf8BOM = append(utf8BOM, []byte("[A]\r\nvalue=old\r\n")...)
	if err := os.WriteFile(filepath.Join(root, "utf8.ini"), utf8BOM, 0o600); err != nil {
		t.Fatal(err)
	}
	utf16Data := encodeText("[A]\r\nvalue=old\r\n", textFormat{encoding: "utf-16le", newline: "\r\n", bom: true})
	if err := os.WriteFile(filepath.Join(root, "utf16.ini"), utf16Data, 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	for _, path := range []string{"utf8.ini", "utf16.ini"} {
		old := "[A]\r\nvalue=old\r\n"
		_, err := sandbox.ApplyPatch("root", []PatchOperation{{
			Type: "write", Path: path, Content: "[A]\nvalue=new\n", ExpectedContent: &old,
		}})
		if err != nil {
			t.Fatalf("ApplyPatch(%s): %v", path, err)
		}
	}

	gotUTF8, _ := os.ReadFile(filepath.Join(root, "utf8.ini"))
	if !bytes.HasPrefix(gotUTF8, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(gotUTF8, []byte("\r\n")) {
		t.Fatalf("UTF-8 BOM/CRLF not preserved: %x", gotUTF8)
	}
	gotUTF16, _ := os.ReadFile(filepath.Join(root, "utf16.ini"))
	if !bytes.HasPrefix(gotUTF16, []byte{0xff, 0xfe}) || !bytes.Contains(gotUTF16, []byte{0x0d, 0x00, 0x0a, 0x00}) {
		t.Fatalf("UTF-16LE BOM/CRLF not preserved: %x", gotUTF16)
	}
}

func TestSandboxPatchPreflightDoesNotPartiallyWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "first.ini"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "second.ini"), []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	firstExpected, wrongExpected := "old", "expected"
	_, err = sandbox.ApplyPatch("root", []PatchOperation{
		{Type: "write", Path: "first.ini", Content: "new", ExpectedContent: &firstExpected},
		{Type: "write", Path: "second.ini", Content: "new", ExpectedContent: &wrongExpected},
	})
	if err == nil {
		t.Fatal("ApplyPatch unexpectedly succeeded")
	}
	got, _ := os.ReadFile(filepath.Join(root, "first.ini"))
	if string(got) != "old" {
		t.Fatalf("first.ini changed after preflight failure: %q", got)
	}
}

func TestSandboxPreparePatchSealsExistingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "existing.ini")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	operations, targets, err := sandbox.PreparePatch("root", []PatchOperation{
		{Type: "write", Path: "existing.ini", Content: "after"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "existing.ini" || operations[0].ExpectedContent == nil ||
		*operations[0].ExpectedContent != "before" {
		t.Fatalf("prepared = %#v, targets = %#v", operations, targets)
	}
	if err := os.WriteFile(path, []byte("changed while awaiting approval"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sandbox.ApplyPatch("root", operations); err == nil {
		t.Fatal("stale approved patch unexpectedly applied")
	}
}

func TestSandboxUpdatePreservesTextFormat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	utf8BOM := []byte{0xef, 0xbb, 0xbf}
	utf8BOM = append(utf8BOM, []byte("[A]\r\nvalue=old\r\nother=1\r\n")...)
	if err := os.WriteFile(filepath.Join(root, "utf8.ini"), utf8BOM, 0o600); err != nil {
		t.Fatal(err)
	}
	utf16Data := encodeText(
		"[A]\r\nvalue=old\r\nother=1\r\n",
		textFormat{encoding: "utf-16le", newline: "\r\n", bom: true},
	)
	if err := os.WriteFile(filepath.Join(root, "utf16.ini"), utf16Data, 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	for _, path := range []string{"utf8.ini", "utf16.ini"} {
		result, err := sandbox.ApplyPatch("root", []PatchOperation{{
			Type: "update", Path: path,
			Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
		}})
		if err != nil {
			t.Fatalf("ApplyPatch(%s): %v", path, err)
		}
		if len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != path {
			t.Fatalf("changed files = %#v", result.ChangedFiles)
		}
		if len(result.Warnings) != 0 {
			t.Fatalf("warnings = %#v", result.Warnings)
		}
	}

	gotUTF8, _ := os.ReadFile(filepath.Join(root, "utf8.ini"))
	if !bytes.HasPrefix(gotUTF8, []byte{0xef, 0xbb, 0xbf}) ||
		string(gotUTF8[3:]) != "[A]\r\nvalue=new\r\nother=1\r\n" {
		t.Fatalf("UTF-8 update changed the format or untouched lines: %q", gotUTF8)
	}
	gotUTF16, _ := os.ReadFile(filepath.Join(root, "utf16.ini"))
	if !bytes.HasPrefix(gotUTF16, []byte{0xff, 0xfe}) {
		t.Fatalf("UTF-16LE BOM not preserved: %x", gotUTF16)
	}
	text, _, binaryFile := decodeText(gotUTF16)
	if binaryFile || text != "[A]\r\nvalue=new\r\nother=1\r\n" {
		t.Fatalf("UTF-16 update = %q", text)
	}
}

func TestSandboxUpdateReportsRelaxedMatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte("value=old   \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	result, err := sandbox.ApplyPatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini",
		Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 ||
		result.Warnings[0] != "mod.ini: hunk 1: matched at line 1 with trailing whitespace ignored" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestSandboxUpdatePreflightFailsBeforeApproval(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("value=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	_, _, err = sandbox.PreparePatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini",
		Hunks: []PatchHunk{{OldLines: []string{"missing=1"}, NewLines: []string{"value=new"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "expected lines not found") {
		t.Fatalf("PreparePatch err = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "value=old\r\n" {
		t.Fatalf("file changed after a rejected hunk: %q", got)
	}
}

func TestSandboxUpdateSealsExistingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("value=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	operations, targets, err := sandbox.PreparePatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini",
		Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "mod.ini" || operations[0].ExpectedContent == nil ||
		*operations[0].ExpectedContent != "value=old\r\n" {
		t.Fatalf("prepared = %#v, targets = %#v", operations, targets)
	}
	if err := os.WriteFile(path, []byte("value=old\r\nadded=1\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sandbox.ApplyPatch("root", operations); err == nil {
		t.Fatal("stale approved update unexpectedly applied")
	}
}

func TestSandboxUpdatePreflightDoesNotPartiallyWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "first.ini"), []byte("value=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "second.ini"), []byte("value=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	_, err = sandbox.ApplyPatch("root", []PatchOperation{
		{
			Type: "update", Path: "first.ini",
			Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
		},
		{
			Type: "update", Path: "second.ini",
			Hunks: []PatchHunk{{OldLines: []string{"missing=1"}, NewLines: []string{"value=new"}}},
		},
	})
	if err == nil {
		t.Fatal("ApplyPatch unexpectedly succeeded")
	}
	got, _ := os.ReadFile(filepath.Join(root, "first.ini"))
	if string(got) != "value=old\r\n" {
		t.Fatalf("first.ini changed after preflight failure: %q", got)
	}
}

func TestSandboxUpdateRejectsMissingFileAndDuplicatePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte("value=old\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	_, err = sandbox.ApplyPatch("root", []PatchOperation{{
		Type: "update", Path: "missing.ini",
		Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), `update "missing.ini": file does not exist`) {
		t.Fatalf("err = %v", err)
	}

	_, err = sandbox.ApplyPatch("root", []PatchOperation{
		{
			Type: "update", Path: "mod.ini",
			Hunks: []PatchHunk{{OldLines: []string{"value=old"}, NewLines: []string{"value=new"}}},
		},
		{Type: "write", Path: "mod.ini", Content: "value=other\r\n"},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate path "mod.ini"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestSandboxUpdateOldStringPreservesUntouchedLines(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("[A]\r\nvalue=old\r\nother=1\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	result, err := sandbox.ApplyPatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini",
		OldString: "[A]\nvalue=old", NewString: "[A]\nvalue=new",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != "mod.ini" {
		t.Fatalf("changed files = %#v", result.ChangedFiles)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "[A]\r\nvalue=new\r\nother=1\r\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestSandboxUpdateOldStringRejectsAmbiguousAndMixedForms(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(path, []byte("[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	_, err = sandbox.ApplyPatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini", OldString: "key=1", NewString: "key=2",
	}})
	if err == nil || !strings.Contains(err.Error(), "oldString matches 2 times") {
		t.Fatalf("ambiguous err = %v", err)
	}

	_, err = sandbox.ApplyPatch("root", []PatchOperation{{
		Type: "update", Path: "mod.ini",
		OldString: "key=1", NewString: "key=2",
		Hunks: []PatchHunk{{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "oldString/newString or hunks, not both") {
		t.Fatalf("mixed form err = %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n" {
		t.Fatalf("file changed after rejected updates: %q", got)
	}
}

func TestSandboxSearchIsCancellable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sandbox.ListFiles(ctx, "root", ".", true, 10); err == nil {
		t.Fatal("ListFiles unexpectedly ignored cancellation")
	}
}

func TestSandboxResolveExistingStaysInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, "mod")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	resolved, err := sandbox.ResolveExisting("root", "mod")
	expected, evalErr := filepath.EvalSymlinks(directory)
	if evalErr != nil {
		t.Fatalf("EvalSymlinks(%q): %v", directory, evalErr)
	}
	if err != nil || resolved != expected {
		t.Fatalf("resolved = %q, expected = %q, %v", resolved, expected, err)
	}
	if _, err := sandbox.ResolveExisting("root", "../outside"); err == nil {
		t.Fatal("path escape unexpectedly accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlink test skipped: %v", err)
		return
	}
	if _, err := sandbox.ResolveExisting("root", "outside-link"); err == nil {
		t.Fatal("symlink escape unexpectedly accepted")
	}
}

func TestSandboxResolveTargetAllowsMissingFileInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	resolved, err := sandbox.ResolveTarget("root", "output/generated.ini")
	expected := filepath.Join(sandbox.Roots()[0].Path, "output", "generated.ini")
	if err != nil || resolved != expected {
		t.Fatalf("resolved = %q, expected = %q, %v", resolved, expected, err)
	}
	if _, err := sandbox.ResolveTarget("root", "../outside.ini"); err == nil {
		t.Fatal("output path escape unexpectedly accepted")
	}
}

func TestSandboxResolversReportUnavailableSandbox(t *testing.T) {
	t.Parallel()
	var sandbox *Sandbox
	if _, err := sandbox.ResolveExisting("root", "mod"); err == nil {
		t.Fatal("nil sandbox unexpectedly resolved an existing path")
	}
	if _, err := sandbox.ResolveTarget("root", "output.ini"); err == nil {
		t.Fatal("nil sandbox unexpectedly resolved a target path")
	}
}

func TestDecodeTextDetectsCarriageReturnNewlines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     []byte
		encoding string
		newline  string
	}{
		{name: "UTF-8 CR-only", data: []byte("first\rsecond\r"), encoding: "utf-8", newline: "\r"},
		{name: "UTF-8 CRLF precedence", data: []byte("first\r\nsecond\r"), encoding: "utf-8", newline: "\r\n"},
		{
			name:     "UTF-16LE CR-only",
			data:     encodeText("first\rsecond\r", textFormat{encoding: "utf-16le", newline: "\r", bom: true}),
			encoding: "utf-16le",
			newline:  "\r",
		},
		{
			name: "UTF-16LE CRLF precedence",
			data: encodeText(
				"first\r\nsecond\r",
				textFormat{encoding: "utf-16le", newline: "\r\n", bom: true},
			),
			encoding: "utf-16le",
			newline:  "\r\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, format, binaryFile := decodeText(test.data)
			if binaryFile {
				t.Fatal("decodeText classified text as binary")
			}
			if format.encoding != test.encoding {
				t.Errorf("encoding = %q, want %q", format.encoding, test.encoding)
			}
			if format.newline != test.newline {
				t.Errorf("newline = %q, want %q", format.newline, test.newline)
			}
		})
	}
}
