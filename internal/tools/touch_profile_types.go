package tools

const (
	touchRuntimeVersion        = "1"
	touchPromptVersion         = "17"
	touchProfileManifestFile   = ".nahida-touch-profile.json"
	touchProfileManifestKind   = "nahida-touch-profile"
	touchObjectMode            = 7
	touchPositionStride        = 40
	touchMaskBands             = 3
	touchZoneChannels          = 12
	touchBakeSamples           = 8
	touchFolderSuffix          = " (Touch)"
	touchConfidenceAutoMin     = 0.55
	touchConfidenceAutoAverage = 0.65
)

var touchShaderFiles = []string{
	"rzm_gs_probe.hlsl",
	"rzm_object_detect.hlsl",
	"rzm_pin_detected.hlsl",
	"rzm_jiggle_screen_state.hlsl",
	"rzm_jiggle_interaction.hlsl",
}

type TouchDrawRange struct {
	FirstIndex    int     `json:"firstIndex"`
	IndexCount    int     `json:"indexCount"`
	BaseVertex    int     `json:"baseVertex"`
	Label         *string `json:"label,omitempty"`
	ConditionText *string `json:"conditionText,omitempty"`
}

type TouchObjectMapEntry struct {
	FirstIndex int    `json:"firstIndex"`
	IndexCount int    `json:"indexCount"`
	ObjectMode int    `json:"objectMode"`
	ObjectID   int    `json:"objectId"`
	Label      string `json:"label"`
}

type TouchComponentAnalysis struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Kind                 string                `json:"kind"`
	InteractiveCandidate bool                  `json:"interactiveCandidate"`
	SupportGrade         string                `json:"supportGrade"`
	SupportReasons       []string              `json:"supportReasons"`
	PositionResourceName string                `json:"positionResourceName"`
	PositionRelativePath string                `json:"positionRelativePath"`
	PositionPath         string                `json:"positionPath"`
	PositionStride       int                   `json:"positionStride"`
	VertexCount          int                   `json:"vertexCount"`
	IndexResourceName    *string               `json:"indexResourceName,omitempty"`
	IndexRelativePath    *string               `json:"indexRelativePath,omitempty"`
	IndexPath            *string               `json:"indexPath,omitempty"`
	IndexRelativePaths   []string              `json:"indexRelativePaths,omitempty"`
	IndexPaths           []string              `json:"indexPaths,omitempty"`
	IndexFormats         []*string             `json:"indexFormats,omitempty"`
	IndexFormat          *string               `json:"indexFormat,omitempty"`
	IndexCount           int                   `json:"indexCount"`
	BlendSectionName     *string               `json:"blendSectionName,omitempty"`
	IBSectionName        *string               `json:"ibSectionName,omitempty"`
	IBHash               *string               `json:"ibHash,omitempty"`
	VariantKey           *string               `json:"variantKey,omitempty"`
	VariantCondition     *string               `json:"variantCondition,omitempty"`
	DrawRanges           []TouchDrawRange      `json:"drawRanges"`
	ObjectMaps           []TouchObjectMapEntry `json:"objectMaps"`
	BlendRelativePath    *string               `json:"blendRelativePath,omitempty"`
	BlendPath            *string               `json:"blendPath,omitempty"`
	BlendStride          *int                  `json:"blendStride,omitempty"`
	Bones                []BlendBoneInfo       `json:"bones"`
}

type TouchModAnalysis struct {
	ModRoot                  string                   `json:"modRoot"`
	SourceRoot               string                   `json:"sourceRoot"`
	ModRootRelativeToSource  string                   `json:"modRootRelativeToSource"`
	INIPath                  string                   `json:"iniPath"`
	INIRelativePath          string                   `json:"iniRelativePath"`
	SourceFilesRelativePaths []string                 `json:"sourceFilesRelativePaths"`
	SupportGrade             string                   `json:"supportGrade"`
	SupportReasons           []string                 `json:"supportReasons"`
	Components               []TouchComponentAnalysis `json:"components"`
	MeshHash                 string                   `json:"meshHash"`
	INIHash                  string                   `json:"iniHash"`
}

type TouchComponentInspection struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Kind                 string                `json:"kind"`
	SupportGrade         string                `json:"supportGrade"`
	InteractiveCandidate bool                  `json:"interactiveCandidate"`
	VertexCount          int                   `json:"vertexCount"`
	IndexCount           int                   `json:"indexCount"`
	VariantKey           *string               `json:"variantKey,omitempty"`
	VariantCondition     *string               `json:"variantCondition,omitempty"`
	ObjectMaps           []TouchObjectMapEntry `json:"objectMaps"`
	HasBlend             bool                  `json:"hasBlend"`
	Bones                []BlendBoneInfo       `json:"bones"`
}

type TouchModInspection struct {
	SessionID                string                     `json:"sessionId"`
	ModRoot                  string                     `json:"modRoot"`
	INIRelativePath          string                     `json:"iniRelativePath"`
	SourceFilesRelativePaths []string                   `json:"sourceFilesRelativePaths"`
	SupportGrade             string                     `json:"supportGrade"`
	SupportReasons           []string                   `json:"supportReasons"`
	Components               []TouchComponentInspection `json:"components"`
}

type TouchAdvancedSettings struct {
	Radius    float64 `json:"radius"`
	Strength  float64 `json:"strength"`
	Damping   float64 `json:"damping"`
	Spring    float64 `json:"spring"`
	MaxOffset float64 `json:"maxOffset"`
	Falloff   float64 `json:"falloff"`
}

type TouchZoneSettings struct {
	MaskStrength        float64               `json:"maskStrength"`
	MaskCurve           float64               `json:"maskCurve"`
	MaskRadiusScale     float64               `json:"maskRadiusScale"`
	MaskCoreAttenuation string                `json:"maskCoreAttenuation"`
	StrengthPreset      string                `json:"strengthPreset"`
	PhysicsPreset       string                `json:"physicsPreset"`
	Advanced            TouchAdvancedSettings `json:"advanced"`
}

type TouchZoneSpec struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Channel    int               `json:"channel"`
	Confidence float64           `json:"confidence"`
	Center     [3]float64        `json:"center"`
	Radius     [3]float64        `json:"radius"`
	Source     string            `json:"source"`
	Settings   TouchZoneSettings `json:"settings"`
	Seeds      []int             `json:"seedVertices,omitempty"`
}

type TouchComponentDraft struct {
	ComponentID string          `json:"componentId"`
	Interactive bool            `json:"interactive"`
	ObjectID    int             `json:"objectId"`
	Zones       []TouchZoneSpec `json:"zones"`
	Confidence  float64         `json:"confidence"`
	Warnings    []string        `json:"warnings"`
}

type TouchProfileLLMSettings struct {
	Protocol  string `json:"protocol"`
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type TouchDraft struct {
	SessionID      string                  `json:"sessionId"`
	CreatedAt      string                  `json:"createdAt"`
	SourceModRoot  string                  `json:"sourceModRoot"`
	Analysis       TouchModAnalysis        `json:"analysis"`
	Components     []TouchComponentDraft   `json:"components"`
	VisionUsed     bool                    `json:"visionUsed"`
	ModelName      string                  `json:"modelName"`
	LLM            TouchProfileLLMSettings `json:"llm"`
	PromptVersion  string                  `json:"promptVersion"`
	RuntimeVersion string                  `json:"runtimeVersion"`
	CanAutoApply   bool                    `json:"canAutoApply"`
	Warnings       []string                `json:"warnings"`
}

type TouchProgressEvent struct {
	SessionID   *string `json:"sessionId,omitempty"`
	Stage       string  `json:"stage"`
	Progress    float64 `json:"progress"`
	Message     string  `json:"message"`
	ComponentID *string `json:"componentId,omitempty"`
}

type TouchMeshDescriptor struct {
	SessionID        string          `json:"sessionId"`
	ComponentID      string          `json:"componentId"`
	TopologyRevision string          `json:"topologyRevision"`
	VertexCount      int             `json:"vertexCount"`
	PositionsURL     string          `json:"positionsUrl"`
	PositionsCount   int             `json:"positionsCount"`
	IndicesURL       *string         `json:"indicesUrl,omitempty"`
	IndexCount       int             `json:"indexCount"`
	Bones            []BlendBoneInfo `json:"bones"`
	BlendStride      *int            `json:"blendStride,omitempty"`
	BlendURL         *string         `json:"blendUrl,omitempty"`
	BlendBytes       int             `json:"blendBytes"`
}

type TouchPreviewZoneDescriptor struct {
	TouchZoneSpec
	WeightOffset int `json:"weightOffset"`
}

type TouchProfilePreviewDescriptor struct {
	SessionID       string                       `json:"sessionId"`
	ComponentID     string                       `json:"componentId"`
	PreviewRevision uint64                       `json:"previewRevision"`
	VertexCount     int                          `json:"vertexCount"`
	WeightsURL      string                       `json:"weightsUrl"`
	WeightsCount    int                          `json:"weightsCount"`
	Zones           []TouchPreviewZoneDescriptor `json:"zones"`
}

type TouchValidationIssue struct {
	Level       string  `json:"level"`
	Code        string  `json:"code"`
	Message     string  `json:"message"`
	ComponentID *string `json:"componentId,omitempty"`
}

type TouchValidationResult struct {
	OK     bool                   `json:"ok"`
	Issues []TouchValidationIssue `json:"issues"`
}

type TouchApplyResult struct {
	SessionID                string                `json:"sessionId"`
	OutputModRoot            string                `json:"outputModRoot"`
	SourceModRoot            string                `json:"sourceModRoot"`
	ReenableSourceOnRollback bool                  `json:"reenableSourceOnRollback"`
	Disabled                 bool                  `json:"disabled"`
	Validation               TouchValidationResult `json:"validation"`
	Warnings                 []string              `json:"warnings"`
}

type TouchRollbackResult struct {
	OutputModRoot   string `json:"outputModRoot"`
	SourceModRoot   string `json:"sourceModRoot"`
	RemovedOutput   bool   `json:"removedOutput"`
	ReenabledSource bool   `json:"reenabledSource"`
}

type TouchJiggleParams struct {
	ObjectID        int     `json:"objectId"`
	Radius          float64 `json:"radius"`
	Strength        float64 `json:"strength"`
	Falloff         float64 `json:"falloff"`
	DragScale       float64 `json:"dragScale"`
	GrabDamping     float64 `json:"grabDamping"`
	GrabSpring      float64 `json:"grabSpring"`
	ReleaseDamping  float64 `json:"releaseDamping"`
	ReleaseSpring   float64 `json:"releaseSpring"`
	ReleaseKick     float64 `json:"releaseKick"`
	MaxOffset       float64 `json:"maxOffset"`
	TargetFollow    float64 `json:"targetFollow"`
	MouseYDirection float64 `json:"mouseYDirection"`
	MouseXDirection float64 `json:"mouseXDirection"`
}

var defaultTouchJiggleParams = TouchJiggleParams{
	Radius: 0.2, Strength: 1.15, Falloff: 1.8, DragScale: 1,
	GrabDamping: 0.86, GrabSpring: 0.176, ReleaseDamping: 0.96,
	ReleaseSpring: 0.055, ReleaseKick: 1.18, MaxOffset: 0.065,
	TargetFollow: 0.12, MouseYDirection: 1, MouseXDirection: 1,
}
