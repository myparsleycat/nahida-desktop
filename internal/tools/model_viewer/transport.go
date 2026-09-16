package modelviewer

type ModelViewerDNFClause struct {
	Var    string `json:"var"`
	Value  string `json:"value"`
	Negate bool   `json:"negate"`
}

type ModelViewerDNF [][]ModelViewerDNFClause

type ModelViewerTextureVariant struct {
	Conditions ModelViewerDNF `json:"conditions"`
	TexKey     string         `json:"texKey"`
}

type ModelViewerShapeTarget struct {
	Var             string `json:"var"`
	PositionsURL    string `json:"positionsUrl"`
	Mode            string `json:"mode,omitempty"`
	LowPositionsURL string `json:"lowPositionsUrl,omitempty"`
}

type ModelViewerPositionVariant struct {
	Conditions  ModelViewerDNF `json:"conditions"`
	GeometryURL string         `json:"geometryUrl"`
}

type ModelViewerBounds struct {
	Min    [3]float64 `json:"min"`
	Max    [3]float64 `json:"max"`
	Center [3]float64 `json:"center"`
	Radius float64    `json:"radius"`
}

type ModelViewerMeshTransport struct {
	ID                  string                       `json:"id"`
	Component           string                       `json:"component"`
	GeometryURL         string                       `json:"geometryUrl"`
	SourceIndicesURL    string                       `json:"sourceIndicesUrl,omitempty"`
	Bounds              *ModelViewerBounds           `json:"bounds,omitempty"`
	Conditions          ModelViewerDNF               `json:"conditions"`
	TexKey              *string                      `json:"texKey"`
	TextureVariants     []ModelViewerTextureVariant  `json:"textureVariants"`
	NormalMapKey        *string                      `json:"normalMapKey"`
	NormalMapVariants   []ModelViewerTextureVariant  `json:"normalMapVariants"`
	LightMapKey         *string                      `json:"lightMapKey"`
	LightMapVariants    []ModelViewerTextureVariant  `json:"lightMapVariants"`
	MaterialMapKey      *string                      `json:"materialMapKey"`
	MaterialMapVariants []ModelViewerTextureVariant  `json:"materialMapVariants"`
	ShapeTargets        []ModelViewerShapeTarget     `json:"shapeTargets"`
	PositionVariants    []ModelViewerPositionVariant `json:"positionVariants"`
}

type ModelViewerTextureTransport struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type ModelViewerVariableValue struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

type ModelViewerMenuGuard struct {
	Var   string `json:"var"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type ModelViewerMenuEffect struct {
	When  *ModelViewerMenuGuard `json:"when,omitempty"`
	Var   string                `json:"var"`
	Value string                `json:"value"`
}

type ModelViewerVariable struct {
	ID            string                     `json:"id"`
	Label         string                     `json:"label"`
	DefaultValue  any                        `json:"defaultValue"`
	Values        []ModelViewerVariableValue `json:"values"`
	Order         int                        `json:"order"`
	Slot          int                        `json:"slot,omitempty"`
	IconPath      string                     `json:"iconPath,omitempty"`
	ControlType   string                     `json:"controlType,omitempty"`
	Slider        *ModelViewerSlider         `json:"slider,omitempty"`
	Effects       []ModelViewerMenuEffect    `json:"effects,omitempty"`
	alwaysVisible bool
}

type ModelViewerStateRule struct {
	Var        string         `json:"var"`
	Value      string         `json:"value"`
	Conditions ModelViewerDNF `json:"conditions"`
}

type ModelViewerAnimationFrame struct {
	Index  int            `json:"index"`
	Time   float64        `json:"time"`
	Values map[string]any `json:"values"`
}

type ModelViewerAnimationClip struct {
	ID          string                      `json:"id"`
	Label       string                      `json:"label"`
	DeformerID  string                      `json:"deformerId,omitempty"`
	VariableIDs []string                    `json:"variableIds"`
	FPS         float64                     `json:"fps"`
	FrameStart  int                         `json:"frameStart"`
	FrameEnd    int                         `json:"frameEnd"`
	Loop        bool                        `json:"loop"`
	Frames      []ModelViewerAnimationFrame `json:"frames"`
}

type ModelViewerComputeBinarySource struct {
	URL        string `json:"url"`
	ByteLength int64  `json:"byteLength"`
	Stride     int    `json:"stride"`
	Encoding   string `json:"encoding,omitempty"`
	sourcePath string
}

type ModelViewerComputeShapePass struct {
	Target       ModelViewerComputeBinarySource `json:"target"`
	PhaseRate    float64                        `json:"phaseRate"`
	WrapAt       float64                        `json:"wrapAt,omitempty"`
	PhaseStart   float64                        `json:"phaseStart,omitempty"`
	PhaseOffset  float64                        `json:"phaseOffset"`
	AngularScale float64                        `json:"angularScale"`
	Amplitude    float64                        `json:"amplitude"`
	Bias         float64                        `json:"bias"`
}

type ModelViewerComputeShapeStage struct {
	Base         ModelViewerComputeBinarySource `json:"base"`
	Target       ModelViewerComputeBinarySource `json:"target"`
	PhaseRate    float64                        `json:"phaseRate"`
	WrapAt       float64                        `json:"wrapAt,omitempty"`
	PhaseStart   float64                        `json:"phaseStart"`
	PhaseOffset  float64                        `json:"phaseOffset"`
	AngularScale float64                        `json:"angularScale"`
	Amplitude    float64                        `json:"amplitude"`
	Bias         float64                        `json:"bias"`
	Duration     float64                        `json:"duration"`
}

type ModelViewerComputePoseSource struct {
	// Empty keeps the legacy packed dual-quaternion interpolation behavior.
	DualQuaternionVariant string                         `json:"dualQuaternionVariant,omitempty"`
	Blend                 ModelViewerComputeBinarySource `json:"blend"`
	Frames                ModelViewerComputeBinarySource `json:"frames"`
	BoneCount             int                            `json:"boneCount"`
	FrameCount            int                            `json:"frameCount"`
}

type ModelViewerComputeDeformerTransport struct {
	MeshSourceIndices map[string]string              `json:"meshSourceIndices,omitempty"`
	Kind              string                         `json:"kind"`
	ID                string                         `json:"id"`
	MeshIDs           []string                       `json:"meshIds"`
	VertexCount       int                            `json:"vertexCount"`
	Base              ModelViewerComputeBinarySource `json:"base"`
	ShapePasses       []ModelViewerComputeShapePass  `json:"shapePasses"`
	ShapeStages       []ModelViewerComputeShapeStage `json:"shapeStages,omitempty"`
	Pose              *ModelViewerComputePoseSource  `json:"pose,omitempty"`
}

type ModelViewerTransport struct {
	MemorySessionID  string                                 `json:"memorySessionId"`
	INIPath          string                                 `json:"iniPath"`
	ModPath          string                                 `json:"modPath"`
	Name             string                                 `json:"name"`
	PreviewPath      *string                                `json:"previewPath,omitempty"`
	MaterialProfile  string                                 `json:"materialProfile,omitempty"`
	Meshes           []ModelViewerMeshTransport             `json:"meshes"`
	Textures         map[string]ModelViewerTextureTransport `json:"textures"`
	Variables        []ModelViewerVariable                  `json:"variables"`
	DefaultState     map[string]any                         `json:"defaultState"`
	StateRules       []ModelViewerStateRule                 `json:"stateRules"`
	UIAssets         ModelViewerUIAssets                    `json:"uiAssets"`
	Animations       []ModelViewerAnimationClip             `json:"animations"`
	ComputeDeformers []ModelViewerComputeDeformerTransport  `json:"computeDeformers"`
}

func normalizeModelViewerTransportConditions(transport *ModelViewerTransport, tracked map[string]bool) {
	if transport == nil {
		return
	}
	for meshIndex := range transport.Meshes {
		mesh := &transport.Meshes[meshIndex]
		mesh.Conditions = normalizeModelViewerDNFWithTracked(mesh.Conditions, tracked)
		for _, variants := range [][]ModelViewerTextureVariant{
			mesh.TextureVariants,
			mesh.NormalMapVariants,
			mesh.LightMapVariants,
			mesh.MaterialMapVariants,
		} {
			for variantIndex := range variants {
				variants[variantIndex].Conditions = normalizeModelViewerDNFWithTracked(
					variants[variantIndex].Conditions,
					tracked,
				)
			}
		}
		for variantIndex := range mesh.PositionVariants {
			mesh.PositionVariants[variantIndex].Conditions = normalizeModelViewerDNFWithTracked(
				mesh.PositionVariants[variantIndex].Conditions,
				tracked,
			)
		}
	}
}
