package modelviewer

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// characterGLTF holds the parts of the character GLB files written by scripts/import-character-motion.mjs and
// scripts/import-character-original.mjs that the viewer reads.
type characterGLTF struct {
	Extras struct {
		Format    int     `json:"format"`
		FPS       float64 `json:"fps"`
		Game      string  `json:"game"`
		Character string  `json:"character"`
		Clips     []struct {
			Name  string `json:"name"`
			Group string `json:"group"`
			Mode  string `json:"mode"`
			// LoopFrom is the second a clip that plays once repeats from after its last frame.
			LoopFrom *float64 `json:"loopFrom"`
		} `json:"clips"`
	} `json:"extras"`
	Nodes []struct {
		Name        string       `json:"name"`
		Translation *[3]float64  `json:"translation"`
		Rotation    *[4]float64  `json:"rotation"`
		Scale       *[3]float64  `json:"scale"`
		Matrix      *[16]float64 `json:"matrix"`
		Children    []int        `json:"children"`
		Mesh        *int         `json:"mesh"`
		Skin        *int         `json:"skin"`
		Extras      struct {
			// Morph names the mesh whose shape keys the node's weight channels drive, and those keys in order.
			Morph *struct {
				Mesh    string   `json:"mesh"`
				Targets []string `json:"targets"`
			} `json:"morph"`
		} `json:"extras"`
	} `json:"nodes"`
	Meshes []struct {
		Name       string `json:"name"`
		Primitives []struct {
			Attributes map[string]int   `json:"attributes"`
			Indices    *int             `json:"indices"`
			Material   *int             `json:"material"`
			Targets    []map[string]int `json:"targets"`
			Extras     struct {
				Role string `json:"role"`
			} `json:"extras"`
		} `json:"primitives"`
		// Weights holds the default weight of each shape key.
		Weights []float32 `json:"weights"`
		Extras  struct {
			TargetNames []string `json:"targetNames"`
			// Blink lists the shape keys that close the eyes.
			Blink []string `json:"blink"`
		} `json:"extras"`
	} `json:"meshes"`
	Materials []struct {
		Name   string `json:"name"`
		Extras struct {
			Shader   string                              `json:"shader"`
			Textures map[string]originalTextureReference `json:"textures"`
		} `json:"extras"`
	} `json:"materials"`
	Skins []struct {
		Joints              []int `json:"joints"`
		InverseBindMatrices *int  `json:"inverseBindMatrices"`
	} `json:"skins"`
	Animations []struct {
		Name     string `json:"name"`
		Channels []struct {
			Sampler int `json:"sampler"`
			Target  struct {
				Node int    `json:"node"`
				Path string `json:"path"`
			} `json:"target"`
		} `json:"channels"`
		Samplers []struct {
			Output int `json:"output"`
		} `json:"samplers"`
	} `json:"animations"`
	Accessors []struct {
		BufferView    *int   `json:"bufferView"`
		ByteOffset    int    `json:"byteOffset"`
		ComponentType int    `json:"componentType"`
		Normalized    bool   `json:"normalized"`
		Count         int    `json:"count"`
		Type          string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
}

func splitCharacterGLB(glb []byte) (*characterGLTF, []byte, error) {
	if len(glb) < 20 || string(glb[:4]) != "glTF" || binary.LittleEndian.Uint32(glb[4:]) != 2 {
		return nil, nil, errors.New("not a glTF 2 binary")
	}
	jsonLength := int(binary.LittleEndian.Uint32(glb[12:]))
	if binary.LittleEndian.Uint32(glb[16:]) != 0x4e4f534a || jsonLength > len(glb)-20 {
		return nil, nil, errors.New("invalid glTF JSON chunk")
	}
	var doc characterGLTF
	if err := json.Unmarshal(glb[20:20+jsonLength], &doc); err != nil {
		return nil, nil, fmt.Errorf("decode glTF JSON: %w", err)
	}
	rest := glb[20+jsonLength:]
	if len(rest) < 8 || binary.LittleEndian.Uint32(rest[4:]) != 0x004e4942 {
		return nil, nil, errors.New("missing glTF binary chunk")
	}
	binLength := int(binary.LittleEndian.Uint32(rest))
	if binLength > len(rest)-8 {
		return nil, nil, errors.New("invalid glTF binary chunk")
	}
	return &doc, rest[8 : 8+binLength], nil
}

// readCharacterAccessor decodes an accessor of components values per element into floats.
func readCharacterAccessor(doc *characterGLTF, bin []byte, index, components int) ([]float32, error) {
	if index < 0 || index >= len(doc.Accessors) {
		return nil, fmt.Errorf("accessor %d does not exist", index)
	}
	accessor := doc.Accessors[index]
	width := map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}[accessor.Type]
	size := map[int]int{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}[accessor.ComponentType]
	if width != components || size == 0 || accessor.BufferView == nil || accessor.Count < 0 {
		return nil, fmt.Errorf("accessor %d has an unexpected layout", index)
	}
	if *accessor.BufferView < 0 || *accessor.BufferView >= len(doc.BufferViews) {
		return nil, fmt.Errorf("accessor %d buffer view does not exist", index)
	}
	view := doc.BufferViews[*accessor.BufferView]
	stride := view.ByteStride
	if stride == 0 {
		stride = width * size
	}
	start := view.ByteOffset + accessor.ByteOffset
	if accessor.Count > 0 {
		end := start + (accessor.Count-1)*stride + width*size
		if view.ByteOffset < 0 || start < view.ByteOffset || end > view.ByteOffset+view.ByteLength || end > len(bin) {
			return nil, fmt.Errorf("accessor %d exceeds its buffer", index)
		}
	}
	out := make([]float32, accessor.Count*width)
	for element := range accessor.Count {
		at := start + element*stride
		for component := range width {
			raw := bin[at+component*size:]
			var value float32
			switch accessor.ComponentType {
			case 5126:
				value = math.Float32frombits(binary.LittleEndian.Uint32(raw))
			case 5125:
				value = float32(binary.LittleEndian.Uint32(raw))
			case 5123:
				value = float32(binary.LittleEndian.Uint16(raw))
				if accessor.Normalized {
					value /= 65535
				}
			case 5122:
				value = float32(int16(binary.LittleEndian.Uint16(raw)))
				if accessor.Normalized {
					value = max(value/32767, -1)
				}
			case 5121:
				value = float32(raw[0])
				if accessor.Normalized {
					value /= 255
				}
			case 5120:
				value = float32(int8(raw[0]))
				if accessor.Normalized {
					value = max(value/127, -1)
				}
			}
			out[element*width+component] = value
		}
	}
	return out, nil
}
