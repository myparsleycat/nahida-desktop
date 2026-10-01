package fixinspection

import (
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/xxmi"
)

func TestSuspendImporterWatchersRestoresInspectionsAndDismissals(t *testing.T) {
	for _, test := range []struct {
		name    string
		move    bool
		dismiss bool
	}{
		{name: "rollback"},
		{name: "move", move: true},
		{name: "dismiss-during-move", move: true, dismiss: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := newMarkerInspectionService()
			t.Cleanup(func() { _ = service.Shutdown() })
			source := filepath.Join(t.TempDir(), "GIMI")
			mod := filepath.Join(source, "Mods", "NeedsFix")
			if err := os.MkdirAll(mod, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mod, "needs-fix"), []byte("pending"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := service.InspectModForFix(t.Context(), mod, "TEST"); err != nil {
				t.Fatal(err)
			}
			resume, err := service.SuspendImporterWatchers(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			resumed := false
			defer func() {
				if !resumed {
					_ = resume(nil)
				}
			}()
			if test.dismiss {
				service.DismissFixInspection(mod)
			}
			target := filepath.Join(t.TempDir(), "GIMI")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(source, "Mods"), filepath.Join(target, "Mods")); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(filepath.Dir(target), ".nahida-gimi-install.backup")
			if err := os.Rename(target, backup); err != nil {
				t.Fatalf("inspection still holds a child directory: %v", err)
			}
			if err := os.Rename(backup, target); err != nil {
				t.Fatal(err)
			}
			var moved []xxmi.ImportedImporter
			final := source
			if test.move {
				moved = []xxmi.ImportedImporter{{Key: "GIMI", PreviousFolder: source, ImporterFolder: target}}
				final = target
			} else if err := os.Rename(filepath.Join(target, "Mods"), filepath.Join(source, "Mods")); err != nil {
				t.Fatal(err)
			}
			resumed = true
			if err := resume(moved); err != nil {
				t.Fatal(err)
			}
			finalMod := filepath.Join(final, "Mods", "NeedsFix")
			service.fixInspectionMu.Lock()
			tracked := service.fixInspections[fixInspectionKey(finalMod)]
			valid := tracked != nil && tracked.warningHidden() == test.dismiss
			service.fixInspectionMu.Unlock()
			if !valid {
				t.Fatal("inspection path or dismissal was not preserved")
			}
			if err := os.Remove(filepath.Join(finalMod, "needs-fix")); err != nil {
				t.Fatal(err)
			}
			if test.dismiss {
				service.RefreshFixInspections(t.Context())
			} else {
				waitForFixInspectionCount(t, service, 0)
			}
		})
	}
}
