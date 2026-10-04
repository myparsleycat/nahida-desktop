package diskio

import (
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestResolveVolumeFollowsTheNearestExistingAncestor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	final, err := platform.FinalPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := volumeName(final)
	if len(want) != 2 || want[1] != ':' {
		t.Skipf("temporary directory %q is not on a drive-letter volume", final)
	}

	for _, path := range []string{dir, filepath.Join(dir, "not", "created", "yet")} {
		if got := resolveVolume(path); got != want {
			t.Fatalf("resolveVolume(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestVolumeNameDropsTheExtendedLengthPrefix(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		`\\?\c:\Mods\a`:             `C:`,
		`D:\Mods`:                   `D:`,
		`\\?\UNC\server\share\Mods`: `\\SERVER\SHARE`,
		`\\server\share\Mods\a.ini`: `\\SERVER\SHARE`,
		// A volume without a drive letter has no name here and stays unlimited.
		`\\?\Volume{0a1b}\Mods\a.ib`: ``,
	} {
		if got := volumeName(path); got != want {
			t.Fatalf("volumeName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSeekPenaltyIgnoresVolumesWithoutADriveLetter(t *testing.T) {
	t.Parallel()
	for _, volume := range []string{"", `\\SERVER\SHARE`, `\\?\VOLUME{00000000-0000-0000-0000-000000000000}`} {
		rotational, err := incursSeekPenalty(volume)
		if rotational || err != nil {
			t.Fatalf("incursSeekPenalty(%q) = %v, %v, want false, nil", volume, rotational, err)
		}
	}
}

// The answer depends on the disk under the temporary directory, and a virtual
// disk may refuse the query, so only an unlimited result on failure is asserted.
func TestSeekPenaltyQueryOfARealVolume(t *testing.T) {
	t.Parallel()
	volume := resolveVolume(t.TempDir())
	rotational, err := incursSeekPenalty(volume)
	t.Logf("volume %s: rotational=%v err=%v", volume, rotational, err)
	if err != nil && rotational {
		t.Fatalf("incursSeekPenalty(%q) reported a rotational disk together with %v", volume, err)
	}
}
