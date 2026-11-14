package modname

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDisabled(t *testing.T) {
	t.Parallel()

	matches := []string{
		"DISABLED Foo",
		"DISABLED_Foo",
		"disabled Foo",
		"Disabled_Foo",
		"disableddisabled Foo",
		"disableddisableddisabled Foo",
		"disabled_disabled_Foo",
		"disabled_disabled_disabled_foo",
	}
	for _, name := range matches {
		if !IsDisabled(name) {
			t.Fatalf("%q should match", name)
		}
	}

	nonMatches := []string{"DisableFoo", "Disable_Foo", "", "disableddisableddisabledFoo"}
	for _, name := range nonMatches {
		if IsDisabled(name) {
			t.Fatalf("%q should not match", name)
		}
	}
}

func TestStripDisabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"DISABLED Foo", "Foo"},
		{"DISABLED_Foo", "Foo"},
		{"disabled_My_Mod", "My_Mod"},
		{"disabled Foo", "Foo"},
		{"Disabled_Foo", "Foo"},
		{"DISABLED Foo  ", "Foo"},
		{"DISABLED  Foo", "Foo"},
		{"My Mod", "My Mod"},
		{"DisableFoo", "DisableFoo"},
		{"仪玄-黑珍珠", "仪玄-黑珍珠"},
		{"DISABLED 仪玄", "仪玄"},
		{"disableddisableddisabledFoo", "disableddisableddisabledFoo"},
		{"disableddisabled Foo", "Foo"},
		{"disabled_disabled_Foo", "Foo"},
		{"disabled disabled Foo", "Foo"},
		{"disableddisableddisabled Foo", "Foo"},
		{"disabled_disabled_disabled_foo", "foo"},
		{"disabled disabled disabled foo", "foo"},
	}
	for _, test := range cases {
		if got := StripDisabled(test.in); got != test.want {
			t.Fatalf("StripDisabled(%q) = %q, want %q", test.in, got, test.want)
		}
		// Mod IDs hash the path form, so it must strip every element exactly as StripDisabled does.
		if got := StripDisabledPath(" DISABLED Group /" + test.in); got != "Group/"+test.want {
			t.Fatalf("StripDisabledPath(%q) = %q, want %q", test.in, got, "Group/"+test.want)
		}
	}
}

func TestDisplayNameKeepsPrefixOnlyNextToSameName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"Foo", "DISABLED foo", "DISABLED Bar"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cases := map[string]string{
		filepath.Join(dir, "Foo"):                "Foo",
		filepath.Join(dir, "DISABLED foo"):       "DISABLED foo",
		filepath.Join(dir, "DISABLED Bar") + `\`: "Bar",
	}
	for path, want := range cases {
		if got := DisplayName(path); got != want {
			t.Fatalf("DisplayName(%q) = %q, want %q", path, got, want)
		}
	}
}
