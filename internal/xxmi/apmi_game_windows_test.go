package xxmi

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestResolveAPMIGame(t *testing.T) {
	t.Parallel()
	const standard, japanese = "AzurPromilia.exe", "AzurPromiliaJP.exe"
	root := t.TempDir()
	standardExe := filepath.Join(root, "Regular Install", standard)
	japaneseExe := filepath.Join(root, "Japanese Install", japanese)
	anotherJapaneseExe := filepath.Join(root, "Another Install", japanese)
	both := filepath.Join(root, "Both Editions")
	standardInBoth := filepath.Join(both, standard)
	japaneseInBoth := filepath.Join(both, japanese)
	staleExe := filepath.Join(root, "Removed Install", standard)
	for _, file := range []string{standardExe, japaneseExe, anotherJapaneseExe, standardInBoth, japaneseInBoth} {
		writeTestFile(t, file, nil)
	}

	for _, tc := range []struct {
		name, folder, override string
		registered             []string
		want                   apmiGameTarget
	}{
		{
			name: "regular install takes precedence", folder: filepath.Dir(standardExe),
			registered: []string{japaneseExe}, want: apmiGameTarget{processName: standard, executable: standardExe},
		},
		{
			name: "Japanese install", folder: filepath.Dir(japaneseExe),
			want: apmiGameTarget{processName: japanese, executable: japaneseExe},
		},
		{
			name: "same folder record identifies the Japanese client", folder: both,
			registered: []string{standardExe, japaneseInBoth},
			want:       apmiGameTarget{processName: japanese, executable: japaneseInBoth},
		},
		{name: "records outside configured folder cannot resolve it", folder: both, registered: []string{japaneseExe}},
		{
			name: "both clients recorded in the same folder", folder: both,
			registered: []string{standardInBoth, japaneseInBoth},
		},
		{
			name: "removed installation is ignored", folder: filepath.Dir(staleExe),
			registered: []string{staleExe, japaneseExe, strings.ToUpper(japaneseExe)},
			want:       apmiGameTarget{processName: japanese, executable: japaneseExe},
		},
		{
			name:       "distinct installs of the same client give only a name",
			registered: []string{japaneseExe, anotherJapaneseExe}, want: apmiGameTarget{processName: japanese},
		},
		{
			name: "existing folder without a client keeps it", folder: root,
			registered: []string{japaneseExe}, want: apmiGameTarget{processName: japanese},
		},
		{name: "different clients recorded", registered: []string{standardExe, japaneseExe}},
		{name: "no installed clients", registered: []string{staleExe}},
		{
			name: "explicit client wins even without a matching folder", folder: filepath.Dir(standardExe),
			override: japanese, registered: []string{standardExe},
			want: apmiGameTarget{processName: japanese},
		},
		{
			name: "explicit client found in records", override: japanese,
			registered: []string{standardExe, japaneseExe},
			want:       apmiGameTarget{processName: japanese, executable: japaneseExe},
		},
		{
			name: "explicit client with matching file", folder: both, override: japanese,
			want: apmiGameTarget{processName: japanese, executable: japaneseInBoth},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := New()
			service.registryGameExecutables = func(context.Context, []string) ([]string, error) {
				return tc.registered, nil
			}
			got, err := service.resolveAPMIGame(t.Context(), ImporterConfig{
				GameFolder: tc.folder, GameProcessExe: tc.override,
			})
			if tc.want.processName == "" {
				if !errors.Is(err, ErrAPMIGameProcessSelectionRequired) {
					t.Fatalf("target = %+v, err = %v; want client selection", got, err)
				}
				return
			}
			sameExecutable := got.executable == "" && tc.want.executable == "" ||
				platform.SamePathFold(got.executable, tc.want.executable)
			if err != nil || got.processName != tc.want.processName || !sameExecutable {
				t.Fatalf("target = %+v, err = %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestAPMIRegistryExecutablePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	executable := filepath.Join(root, "AzurPromilia.exe", "Game Install", "AzurPromiliaJP.exe")
	for _, tc := range []struct {
		name, record, want string
	}{
		{name: "execution record", record: executable, want: executable},
		{name: "MUI label", record: strings.ToUpper(executable) + ".FriendlyAppName", want: executable},
		{name: "different executable", record: filepath.Join(root, "OtherAzurPromiliaJP.exe")},
		{name: "backup file", record: executable + ".bak"},
		{name: "relative path", record: "AzurPromiliaJP.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := registryGameExecutablePath(tc.record, []string{"AzurPromilia.exe", "AzurPromiliaJP.exe"})
			if (got != "" || tc.want != "") && !platform.SamePathFold(got, tc.want) {
				t.Fatalf("path = %q; want %q", got, tc.want)
			}
		})
	}
}
