package mod

import (
	"context"
	"os"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

func (m *Mod) GetPreviousFocusedGame(ctx context.Context) (*string, error) {
	if m == nil || m.native == nil {
		return nil, nil
	}
	currentPID := os.Getpid()
	currentName := strings.ToLower(m.native.ProcessName(uint32(currentPID)))
	previous := m.native.PreviousPIDs(uint32(currentPID))
	if len(previous) == 0 {
		return nil, nil
	}
	games, err := m.GetGames(ctx)
	if err != nil {
		if m.log != nil {
			_ = infra.ReportError(m.log, err, "Mod:previousFocusedGame", infra.Diagnostic{Severity: infra.DiagnosticError, Operation: "Mod:previousFocusedGame", Stage: "background"})
		}
		return nil, nil
	}
	for _, pid := range previous {
		processName := m.native.ProcessName(pid)
		if processName == "" {
			continue
		}
		lower := strings.ToLower(processName)
		if currentName != "" && strings.Contains(lower, currentName) {
			continue
		}
		if strings.Contains(lower, "explorer") {
			continue
		}
		for _, candidate := range xxmi.GameMatchCases {
			isGame := false
			for _, keyword := range candidate.Keywords {
				if strings.Contains(lower, keyword) {
					isGame = true
					break
				}
			}
			if !isGame {
				continue
			}
			for _, game := range games {
				lowerGame := strings.ToLower(game.Game)
				for _, keyword := range candidate.Keywords {
					if strings.Contains(lowerGame, keyword) {
						name := game.Game
						return &name, nil
					}
				}
			}
		}
	}
	return nil, nil
}
