// Package liquidemitter attaches LiquidGravityProbe fluid emitters to EFMI
// character mods. The emitter runtime is one global mod that modruntime
// installs into the game's mod root; each character draw that emits gets a CaptureSurface hook
// in its INI and a profile row the runtime reads on F10.
package liquidemitter

import "nahida.live/desktop/internal/modruntime"

const (
	// runtimeVersion moves with any change under runtime/ or games/, so existing
	// installs read as outdated and are updated.
	runtimeVersion    = "2"
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

	ErrorNotEFMI         = "LIQUID_NOT_EFMI"
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
}

// Look is the liquid of one mod's character, applied over the runtime's
// global settings.
type Look struct {
	// StrengthOffset shifts the global strength level by whole levels; the
	// sum is kept within the runtime's levels 0 to 7.
	StrengthOffset int `json:"strengthOffset"`
	// CustomColor replaces the global tint and absorption depth with Tint and
	// AbsorptionMM.
	CustomColor  bool       `json:"customColor"`
	Tint         [3]float64 `json:"tint"`
	AbsorptionMM float64    `json:"absorptionMm"`
}

type Inspection struct {
	ModPath  string            `json:"modPath"`
	Runtime  modruntime.Status `json:"runtime"`
	Emitters []Emitter         `json:"emitters"`
	// SourcePath is the original mod this tool copied ModPath from, or empty
	// when ModPath is not such a copy.
	SourcePath string `json:"sourcePath"`
	Look       Look   `json:"look"`
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
}

type LookInput struct {
	ModPath string `json:"modPath"`
	Look    Look   `json:"look"`
}

// RemoveInput removes the listed emitters of a mod; no PIDs removes them all.
type RemoveInput struct {
	ModPath string `json:"modPath"`
	PIDs    []int  `json:"pids"`
}
