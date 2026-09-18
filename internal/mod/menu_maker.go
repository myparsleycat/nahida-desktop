package mod

import (
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/menumaker"
)

func (m *Mod) repairEnabledMenuMakerSidecars(groupPath string, reports ...func(error)) {
	entries, err := os.ReadDir(groupPath)
	if err != nil {
		reportMenuMakerRepairFailure(err, reports)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || isDisabled(entry.Name()) {
			continue
		}
		reportMenuMakerRepairFailure(
			menumaker.RepairRelocatedSidecars(filepath.Join(groupPath, entry.Name())),
			reports,
		)
	}
}

func reportMenuMakerRepairFailure(err error, reports []func(error)) {
	if err == nil {
		return
	}
	for _, report := range reports {
		if report != nil {
			report(err)
		}
	}
}
