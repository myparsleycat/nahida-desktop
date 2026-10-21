package liquidemitter

import (
	"slices"
	"strings"

	"nahida.live/desktop/internal/modruntime"
)

// game is an importer the emitter supports. Its adapter in games/<dir> fills
// the templates of the core runtime/Probe.ini and installs its own files next
// to the core's; runtime/Probe.ini documents what an adapter defines.
type game struct {
	importer string
	dir      string
	// meshUnit is the length in meters of one unit of the mods' meshes, in
	// which the viewer shows them and anchors are edited.
	meshUnit float64
	// meshDown points down a standing character in the mods' mesh space: the
	// raw buffers of GIMI and SRMI are Unity's Y up, those of WWMI and ZZMI
	// are Z up, and EFMI keeps the Z up the editor always assumed.
	meshDown [3]float64
}

var (
	yDown = [3]float64{0, -1, 0}
	zDown = [3]float64{0, 0, -1}
)

var games = []game{
	{importer: "EFMI", dir: "efmi", meshUnit: 1, meshDown: zDown},
	{importer: "GIMI", dir: "gimi", meshUnit: 1, meshDown: yDown},
	{importer: "SRMI", dir: "srmi", meshUnit: 1, meshDown: yDown},
	{importer: "WWMI", dir: "wwmi", meshUnit: .01, meshDown: zDown},
	{importer: "ZZMI", dir: "zzmi", meshUnit: 1, meshDown: zDown},
}

func gameFor(importer string) (game, bool) {
	index := slices.IndexFunc(games, func(g game) bool { return strings.EqualFold(g.importer, importer) })
	if index < 0 {
		return game{}, false
	}
	return games[index], true
}

// Importers lists the importers whose mods the emitter can hook.
func Importers() []string {
	importers := make([]string, len(games))
	for index, g := range games {
		importers[index] = g.importer
	}
	return importers
}

// RuntimeSpecs is the LiquidGravityProbe runtime of every supported game.
func RuntimeSpecs() []modruntime.Spec {
	specs := make([]modruntime.Spec, len(games))
	for index, g := range games {
		specs[index] = runtimeSpec(g)
	}
	return specs
}
