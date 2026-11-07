package mod

import "testing"

func TestDisabledFileSuffix(t *testing.T) {
	t.Parallel()

	if !isDisabledFile("preview.png.disabled") {
		t.Fatal("expected preview.png.disabled to be disabled")
	}
	if !isDisabledFile("preview.png.DISABLED") {
		t.Fatal("expected preview.png.DISABLED to be disabled")
	}
	if isDisabledFile("preview.png") {
		t.Fatal("preview.png should not be disabled")
	}
	if got := stripDisabledFileSuffix("preview.png.disabled"); got != "preview.png" {
		t.Fatalf("stripDisabledFileSuffix = %q", got)
	}
	if got := stripDisabledFileSuffix("preview.png"); got != "preview.png" {
		t.Fatalf("stripDisabledFileSuffix unchanged = %q", got)
	}
}

func TestIsDisabledFolderName(t *testing.T) {
	t.Parallel()

	if !isDisabledFolderName("DISABLED Foo") || !isDisabledFolderName("DISABLED_Foo") {
		t.Fatal("expected disabled folder names")
	}
	if isDisabledFolderName("Foo") {
		t.Fatal("Foo should not be disabled")
	}
}

func TestRestoreDisabledPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source, name, want string
	}{
		{"DISABLED OldName", "NewName", "DISABLED NewName"},
		{"DISABLED_OldName", "NewName", "DISABLED_NewName"},
		{"disabled OldName", "NewName", "disabled NewName"},
		{"OldName", "NewName", "NewName"},
	}
	for _, test := range cases {
		if got := restoreDisabledPrefix(test.source, test.name); got != test.want {
			t.Fatalf("restoreDisabledPrefix(%q, %q) = %q, want %q", test.source, test.name, got, test.want)
		}
	}
}

func TestNormalizeRelativePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"DISABLED Foo/Bar", "foo/bar"},
		{"DISABLED_Foo/Bar", "foo/bar"},
		{"DISABLED Foo\\Bar", "foo/bar"},
		{"Foo\\DISABLED Bar/Baz", "foo/bar/baz"},
	}
	for _, test := range cases {
		if got := normalizeRelativePath(test.in); got != test.want {
			t.Fatalf("normalizeRelativePath(%q) = %q, want %q", test.in, got, test.want)
		}
	}
	if normalizeRelativePath("DISABLED MyMod") != normalizeRelativePath("MyMod") {
		t.Fatal("enabled and disabled variants should share a key")
	}
}

func TestManualSubGroupSegmentMatches(t *testing.T) {
	t.Parallel()

	if !manualSubGroupSegmentMatches("Foo", "foo") {
		t.Fatal("expected exact match")
	}
	if !manualSubGroupSegmentMatches("DISABLED Foo", "foo") || !manualSubGroupSegmentMatches("DISABLED_Foo", "foo") {
		t.Fatal("expected stripped prefix match")
	}
	if manualSubGroupSegmentMatches("DISABLED Foo", "bar") {
		t.Fatal("different names should not match")
	}
	if manualSubGroupSegmentMatches("Foo", "disabled foo") {
		t.Fatal("stored segment with prefix should not match a bare name")
	}
}
