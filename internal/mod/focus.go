package mod

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

// GetPreviousFocusedGame returns the registered game whose process is running, preferring the most
// recently focused one when several are.
func (m *Mod) GetPreviousFocusedGame(ctx context.Context) (*string, error) {
	if m == nil || m.gameProcesses == nil {
		return nil, nil
	}
	games, err := m.listGames(ctx)
	if err != nil {
		m.reportFocusedGameError(err, infra.DiagnosticError, "background")
		return nil, nil
	}
	if len(games) == 0 {
		return nil, nil
	}

	running, err := m.gameProcesses(ctx, nteProcessNames(games))
	if err != nil {
		m.reportFocusedGameError(err, infra.DiagnosticWarn, "snapshot")
		return nil, nil
	}
	if len(running) == 0 {
		return nil, nil
	}
	var focused []uint32
	if m.native != nil {
		focused = m.native.PreviousPIDs(uint32(os.Getpid()))
	}
	return runningGame(games, focused, running), nil
}

func (m *Mod) reportFocusedGameError(err error, severity infra.DiagnosticSeverity, stage string) {
	if m.log == nil {
		return
	}
	_ = infra.ReportError(m.log, err, "Mod:previousFocusedGame", infra.Diagnostic{
		Severity: severity, Operation: "Mod:previousFocusedGame", Stage: stage,
	})
}

// nteProcessNames watches the NTE game executable and the executable registered for each NTE game, matched
// the way runningGame matches them, since NTE has no XXMI settings to name its process.
func nteProcessNames(games []GameConfig) map[string][]string {
	names := []string{nteExecutableName}
	found := false
	for _, game := range games {
		if !gameUsesImporter(game, db.NTEImporter) {
			continue
		}
		found = true
		if game.GameExecutablePath == nil || strings.TrimSpace(*game.GameExecutablePath) == "" {
			continue
		}
		name := filepath.Base(strings.TrimSpace(*game.GameExecutablePath))
		if !slices.ContainsFunc(names, func(known string) bool { return strings.EqualFold(known, name) }) {
			names = append(names, name)
		}
	}
	if !found {
		return nil
	}
	return map[string][]string{db.NTEImporter: names}
}

// runningGame picks the game for the running importers, trying recently focused processes first and then the
// rest in game order.
func runningGame(games []GameConfig, focused []uint32, running map[uint32]string) *string {
	for _, pid := range focused {
		key, ok := running[pid]
		if !ok {
			continue
		}
		if index := slices.IndexFunc(
			games,
			func(game GameConfig) bool { return gameUsesImporter(game, key) },
		); index >= 0 {
			return &games[index].Game
		}
	}

	for index, game := range games {
		for _, key := range running {
			if gameUsesImporter(game, key) {
				return &games[index].Game
			}
		}
	}
	return nil
}

func gameUsesImporter(game GameConfig, key string) bool {
	if game.Importer == nil {
		return false
	}
	if key == db.NTEImporter {
		return isNTEImporter(game.Importer)
	}
	return strings.EqualFold(strings.TrimSpace(*game.Importer), key)
}
