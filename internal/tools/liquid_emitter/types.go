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
	runtimeVersion    = "22"
	runtimeNamespace  = "LiquidGravityProbe"
	runtimeName       = "LiquidGravityProbe"
	profilesDir       = "Profiles"
	profileIndexFile  = "_index.json"
	profileLoaderFile = "surface/ProfileLoader.ini"
	modMarkerFile     = ".nahida-liquid.json"
	// The loader reads every .ini below Mods, so the backup must not end in .ini.
	backupSuffix = ".nhd-liquid-orig"

	// SurfaceProfiles holds 20480 float4 rows, PROFILE_ROWS (5) per profile.
	maxProfiles = 4096
	// LoadProfilesJSON rejects a whole profile file when any offset axis exceeds 25 cm.
	maxOffsetMeters = 0.25
	// Slots.hlsl slotWidth clamps a look's stream width to the same range.
	minStreamWidth = 0.05
	maxStreamWidth = 2
	// Slots.hlsl slotViscosity clamps a look's viscosity to the same range.
	minViscosity = 0.1
	maxViscosity = 10
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

// ApplyInPlace hooks the mod's own INI and keeps a backup next to it.
// ApplyNewMod copies the mod to a new folder, disables the original and
// hooks the copy, as the touch profile tool does.
const (
	ApplyInPlace = "in_place"
	ApplyNewMod  = "new_mod"
)

// EmitterAnchor is the editor state of one emitter in source mesh space
// (Z up). Point is the surface point before Offset is added; the face fields
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
	// StrengthOffset shifts the global strength level by whole levels; the
	// sum is kept within the runtime's levels 0 to 7.
	StrengthOffset int `json:"strengthOffset"`
	// CustomColor replaces the global tint and absorption depth with Tint and
	// AbsorptionMM.
	CustomColor  bool       `json:"customColor"`
	Tint         [3]float64 `json:"tint"`
	AbsorptionMM float64    `json:"absorptionMm"`
	// StreamWidth scales the stream's cross-section; 1 keeps the runtime's.
	// A profile saved before the setting reads as 1.
	StreamWidth float64 `json:"streamWidth"`
	// Viscosity scales how thick the liquid is over the runtime's water-like
	// one; 1 keeps the runtime's. A profile saved before the setting reads as 1.
	Viscosity float64 `json:"viscosity"`
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
	// Renumbered lists the slots this call moved the mod's emitters to: a
	// folder copied with its marker gets slots of its own on its first change,
	// so a slot the caller listed before may now name another emitter or none.
	Renumbered []PIDChange `json:"renumbered"`
	// Applied is the slot of the emitter Apply hooked or SetLook changed.
	Applied *int `json:"applied"`
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
