package ui

import (
	"context"
	"testing"

	"nahida.live/desktop/internal/mod/character"
)

func TestViewerCharacterChangeDiscardsPreviousLoads(t *testing.T) {
	t.Parallel()
	motion, stopMotion := context.WithCancel(t.Context())
	original, stopOriginal := context.WithCancel(t.Context())
	defer stopMotion()
	defer stopOriginal()
	v := &ModelViewer{
		character:      &viewerCharacter{game: "GIMI", Character: character.Character{ID: "diluc"}},
		motionCancel:   stopMotion,
		originalCancel: stopOriginal,
		expression:     viewerExpression{preset: "smile"},
		form:           "costume",
		state:          map[string]any{"outfit": 1},
	}
	previous := v.downloads.begin(downloadMotion, nil)
	previous.report("old", 1, 2, false)

	v.applyCharacter(&viewerCharacter{game: "GIMI", Character: character.Character{ID: "furina"}})
	if motion.Err() == nil || original.Err() == nil {
		t.Fatal("character change left the previous character's data loading")
	}
	if v.character.ID != "furina" || v.expression.preset != "" || v.form != "costume" || v.state["outfit"] != 1 {
		t.Fatalf("character or mod state after switch = %s, %+v, %s, %v",
			v.character.ID, v.expression, v.form, v.state)
	}
	current := v.downloads.begin(downloadMotion, nil)
	defer current.end()
	current.report("new", 1, 4, false)
	previous.report("old", 2, 2, false)
	previous.end()
	if got := v.downloads.shown(); len(got) != 1 || got[0].done != 1 || got[0].total != 4 {
		t.Fatalf("progress after old download completed = %+v; want only 1 of 4 bytes for new character", got)
	}
}
