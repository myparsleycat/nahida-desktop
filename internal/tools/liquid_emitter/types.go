// Package liquidemitter attaches LiquidGravityProbe fluid emitters to
// character mods of the games in games.go. The emitter runtime is one global
// mod that modruntime installs into the game's mod root; each character draw
// that emits gets a CaptureSurface hook in its INI and a profile row the
// runtime reads on F10.
package liquidemitter

import "nahida.live/desktop/internal/modruntime"

const (
	// runtimeVersion moves with any change under runtime/ or games/, so existing
	// installs read as outdated and are updated.
	runtimeVersion    = "30"
	runtimeNamespace  = "LiquidGravityProbe"
	runtimeName       = "LiquidGravityProbe"
	profilesDir       = "Profiles"
	profileIndexFile  = "_index.json"
	profileLoaderFile = "surface/ProfileLoader.ini"
	modMarkerFile     = ".nahida-liquid.json"
	// The loader reads every .ini below Mods, so the backup must not end in .ini.
	backupSuffix = ".nhd-liquid-orig"

	// SurfaceProfiles holds 24576 float4 rows, PROFILE_ROWS (6) per profile.
	maxProfiles = 4096
	// LoadProfilesJSON rejects a whole profile file when any offset axis exceeds 25 cm.
	maxOffsetMeters = 0.25
	// Slots.hlsl slotViscosity clamps a look's viscosity to the same range.
	minViscosity = 0.1
	maxViscosity = 10
	// Slots.hlsl slotDrive and slotAmount clamp a look's force and amount to
	// the same ranges.
	minForce  = 0.15
	maxForce  = 2.35
	minAmount = 0.02
	maxAmount = 2
	// Each emitter takes one of the runtime's SLOT_COUNT (8) slots while its
	// character is drawn, so a mod may not hold more than half of them.
	maxEmitters = 4

	ErrorUnsupportedGame = "LIQUID_UNSUPPORTED_GAME"
	ErrorRuntimeForeign  = "LIQUID_RUNTIME_FOREIGN"
	ErrorViewerSession   = "LIQUID_VIEWER_SESSION"
	ErrorUnsupportedDraw = "LIQUID_UNSUPPORTED_DRAW"
	ErrorDrawMismatch    = "LIQUID_DRAW_MISMATCH"
	ErrorDrawNotFound    = "LIQUID_DRAW_NOT_FOUND"
	ErrorSharedDraw      = "LIQUID_SHARED_DRAW"
	ErrorInvalidAnchor   = "LIQUID_INVALID_ANCHOR"
	ErrorOffsetLimit     = "LIQUID_OFFSET_LIMIT"
	ErrorSlotsExhausted  = "LIQUID_SLOTS_EXHAUSTED"
	ErrorUnsupportedINI  = "LIQUID_UNSUPPORTED_INI"
	ErrorRuntimeDisabled = "LIQUID_RUNTIME_DISABLED"
	ErrorDamagedHook     = "LIQUID_DAMAGED_HOOK"
	ErrorInvalidMode     = "LIQUID_INVALID_MODE"
	ErrorGeneratedMod    = "LIQUID_GENERATED_MOD"
	ErrorNotGenerated    = "LIQUID_NOT_GENERATED"
	ErrorSourceMissing   = "LIQUID_SOURCE_MISSING"
	ErrorInvalidLook     = "LIQUID_INVALID_LOOK"
	ErrorNoEmitters      = "LIQUID_NO_EMITTERS"
	ErrorEmitterLimit    = "LIQUID_EMITTER_LIMIT"
	ErrorEmitterNotFound = "LIQUID_EMITTER_NOT_FOUND"

	diagnosticWhere = "LiquidEmitter"
	operationPrefix = "liquid-emitter-"
)

// The nozzle kinds of a Look. Each gives Force, Amount and Viscosity their own
// meaning; Look documents them.
const (
	NozzleSpray = "spray"
	NozzleJet   = "jet"
	NozzleOoze  = "ooze"
)

// ApplyInPlace hooks the mod's own INI and keeps a backup next to it.
// ApplyNewMod copies the mod to a new folder, disables the original and
// hooks the copy, as the touch profile tool does.
const (
	ApplyInPlace = "in_place"
	ApplyNewMod  = "new_mod"
)

// EmitterAnchor is the editor state of one emitter in source mesh space
// (see Inspection.MeshDown). Point is the surface point before Offset is added; the face fields
// keep the viewer corner order so the editor can apply the anchor again.
type EmitterAnchor struct {
	MeshID            string    `json:"meshId"`
	FaceIndex         int       `json:"faceIndex"`
	FaceVertices      []uint32  `json:"faceVertices"`
	TrianglePositions []float64 `json:"trianglePositions"`
	Barycentric       []float64 `json:"barycentric"`
	Point             []float64 `json:"point"`
	Offset            []float64 `json:"offset"`
	Direction         []float64 `json:"direction"`
}

type Emitter struct {
	PID     int    `json:"pid"`
	INIPath string `json:"iniPath"`
	Section string `json:"section"`
	// Hooked is false when the INI no longer carries this emitter's hook.
	Hooked bool          `json:"hooked"`
	Anchor EmitterAnchor `json:"anchor"`
	Look   Look          `json:"look"`
}

// Look is the liquid of one emitter, applied over the runtime's global
// settings.
type Look struct {
	// Force scales the nozzle pressure, which sets how fast and how widely the
	// liquid sprays; 1 is the runtime's former default strength level. A
	// profile saved before the setting reads as 1.
	Force float64 `json:"force"`
	// Amount scales how much liquid the emitter releases; 1 keeps the
	// runtime's. The stream's thickness follows from it: less flow draws a
	// thinner stream down to 0.1, below which it breaks into drops, and more
	// flow emits more particles through a wider outlet. A profile saved before
	// the setting reads as 1.
	Amount float64 `json:"amount"`
	// CustomColor replaces the global tint and absorption depth with Tint and
	// AbsorptionMM.
	CustomColor  bool       `json:"customColor"`
	Tint         [3]float64 `json:"tint"`
	AbsorptionMM float64    `json:"absorptionMm"`
	// Viscosity scales how thick the liquid is over the runtime's water-like
	// one; 1 keeps the runtime's. A profile saved before the setting reads as 1.
	Viscosity float64 `json:"viscosity"`
	// Nozzle is NozzleSpray, NozzleJet or NozzleOoze; a profile saved before
	// the setting reads as NozzleSpray, the runtime's former nozzle, for which
	// the fields above mean what they say. A jet shoots a thin straight stream
	// whose speed Force sets. Ooze emits no stream: the liquid gathers at the
	// outlet as a drop that swells, stretches and breaks off: Amount sets how
	// fast it fills, Force how large it grows before it breaks off and how hard
	// it leaves, and Viscosity how slowly it stretches and runs.
	Nozzle string `json:"nozzle"`
	// Scattering clouds the liquid, from 0 for the clear liquid the runtime
	// draws to 1 for a milky one, without changing its color or absorption
	// depth. A profile saved before the setting reads as 0.
	Scattering float64 `json:"scattering"`
	// Roughness widens and softens the liquid's reflections and highlights,
	// from 0 for the runtime's mirror-like surface to 1. A profile saved before
	// the setting reads as 0.
	Roughness float64 `json:"roughness"`
}

type Inspection struct {
	ModPath string `json:"modPath"`
	// Importer names the game whose runtime Runtime describes, as the mod
	// runtime manager addresses it.
	Importer string            `json:"importer"`
	Runtime  modruntime.Status `json:"runtime"`
	Emitters []Emitter         `json:"emitters"`
	// SourcePath is the original mod this tool copied ModPath from, or empty
	// when ModPath is not such a copy.
	SourcePath string `json:"sourcePath"`
	// MeshUnit is the length in meters of one unit of the mod's meshes, in
	// which anchors and their offsets are given.
	MeshUnit float64 `json:"meshUnit"`
	// MeshDown is the unit vector pointing down a standing character in the
	// mod's mesh space, which is Y up for some games and Z up for others.
	MeshDown [3]float64 `json:"meshDown"`
	// Renumbered lists the slots this call moved the mod's emitters to: a
	// folder copied with its marker gets slots of its own on its first change,
	// so a slot the caller listed before may now name another emitter or none.
	Renumbered []PIDChange `json:"renumbered"`
	// Applied is the slot of the emitter Apply hooked or SetLook changed.
	Applied *int `json:"applied"`
	// DefaultLook is the look an emitter added without one starts from.
	DefaultLook Look `json:"defaultLook"`
}

type PIDChange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// ApplyInput describes a picked face of a model viewer mesh. FaceVertices,
// Barycentric and TrianglePositions follow the viewer's corner order, which
// may differ from the index buffer order.
type ApplyInput struct {
	ModPath           string    `json:"modPath"`
	ViewerSessionID   string    `json:"viewerSessionId"`
	MeshID            string    `json:"meshId"`
	FaceIndex         int       `json:"faceIndex"`
	FaceVertices      []uint32  `json:"faceVertices"`
	Barycentric       []float64 `json:"barycentric"`
	TrianglePositions []float64 `json:"trianglePositions"`
	Point             []float64 `json:"point"`
	Offset            []float64 `json:"offset"`
	Direction         []float64 `json:"direction"`
	// Mode is ApplyInPlace or ApplyNewMod; empty means ApplyInPlace.
	Mode string `json:"mode"`
	// PID names the mod's emitter to move to the picked face, which keeps its
	// slot and look; nil adds an emitter.
	PID *int `json:"pid"`
	// Look, when set, replaces the look the emitter keeps or starts from.
	Look *Look `json:"look"`
}

type LookInput struct {
	ModPath string `json:"modPath"`
	PID     int    `json:"pid"`
	Look    Look   `json:"look"`
}

// RemoveInput removes the listed emitters of a mod; no PIDs removes them all.
type RemoveInput struct {
	ModPath string `json:"modPath"`
	PIDs    []int  `json:"pids"`
}
