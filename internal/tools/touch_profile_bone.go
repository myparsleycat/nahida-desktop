package tools

import (
	"errors"
	"fmt"
	"math"
)

const (
	defaultBoneWeightThreshold    = 0.01
	defaultBoneWeightThresholdMax = 1.0
)

type TouchBoneZoneSelection struct {
	BoneID  uint32  `json:"boneId"`
	Channel *int    `json:"channel"`
	Label   *string `json:"label,omitempty"`
}

type TouchBoneComponentSelection struct {
	ComponentID string                   `json:"componentId"`
	Zones       []TouchBoneZoneSelection `json:"zones"`
}

func defaultTouchZoneSettings() TouchZoneSettings {
	return TouchZoneSettings{
		MaskStrength: 1, MaskCurve: 1, MaskRadiusScale: 1,
		MaskCoreAttenuation: "off", StrengthPreset: "normal", PhysicsPreset: "normal",
		Advanced: TouchAdvancedSettings{Radius: 0.2, Strength: 1.15, Damping: 0.86, Spring: 0.176, MaxOffset: 0.065, Falloff: 1.8},
	}
}

func normalizeTouchZoneSettings(input TouchZoneSettings) (TouchZoneSettings, error) {
	if input.StrengthPreset != "light" && input.StrengthPreset != "normal" && input.StrengthPreset != "strong" {
		return input, fmt.Errorf("invalid touch strength preset: %s", input.StrengthPreset)
	}
	if input.PhysicsPreset != "soft" && input.PhysicsPreset != "normal" && input.PhysicsPreset != "firm" && input.PhysicsPreset != "custom" {
		return input, fmt.Errorf("invalid touch physics preset: %s", input.PhysicsPreset)
	}
	if err := finiteRange(input.MaskStrength, 0, 2, "Touch mask strength out of range"); err != nil {
		return input, err
	}
	if err := finiteRange(input.MaskCurve, 0, 2, "Touch mask curve out of range"); err != nil {
		return input, err
	}
	if err := finiteRange(input.MaskRadiusScale, 0.1, 2, "Touch mask radius scale out of range"); err != nil {
		return input, err
	}
	if input.MaskCoreAttenuation != "off" && input.MaskCoreAttenuation != "linear" && input.MaskCoreAttenuation != "sqrt" && input.MaskCoreAttenuation != "pow" {
		return input, fmt.Errorf("invalid touch mask core attenuation: %q", input.MaskCoreAttenuation)
	}
	ranges := []struct {
		value, low, high float64
		name             string
	}{
		{input.Advanced.Radius, .02, 1, "radius"}, {input.Advanced.Strength, .1, 3, "strength"},
		{input.Advanced.Damping, .01, .99, "damping"}, {input.Advanced.Spring, .01, 1, "spring"},
		{input.Advanced.MaxOffset, .005, .3, "maxOffset"}, {input.Advanced.Falloff, .1, 4, "falloff"},
	}
	for _, entry := range ranges {
		if math.IsNaN(entry.value) || math.IsInf(entry.value, 0) {
			return input, fmt.Errorf("invalid touch advanced setting: %s", entry.name)
		}
		if entry.value < entry.low || entry.value > entry.high {
			return input, fmt.Errorf("touch advanced setting out of range: %s", entry.name)
		}
	}
	return input, nil
}

func finiteRange(value, low, high float64, message string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < low || value > high {
		return errors.New(message)
	}
	return nil
}

func resolveTouchJiggleParams(settings TouchZoneSettings, objectID int) TouchJiggleParams {
	multiplier := map[string]float64{"light": .75, "normal": 1, "strong": 1.3}[settings.StrengthPreset]
	params := defaultTouchJiggleParams
	params.ObjectID = objectID
	params.Radius = settings.Advanced.Radius
	params.Strength = settings.Advanced.Strength * multiplier
	params.Falloff = settings.Advanced.Falloff
	params.GrabDamping = settings.Advanced.Damping
	params.GrabSpring = settings.Advanced.Spring
	params.ReleaseDamping = math.Min(.99, settings.Advanced.Damping+.1)
	params.ReleaseSpring = settings.Advanced.Spring * (defaultTouchJiggleParams.ReleaseSpring / defaultTouchJiggleParams.GrabSpring)
	params.MaxOffset = settings.Advanced.MaxOffset
	return params
}

func analyzeTouchComponentBones(component TouchComponentAnalysis, positions []float32, blendBytes []byte, blendStride int, selections []TouchBoneZoneSelection, threshold [2]float64, objectID int) TouchComponentDraft {
	unsupported := func(warning string) TouchComponentDraft {
		return TouchComponentDraft{ComponentID: component.ID, ObjectID: objectID, Zones: []TouchZoneSpec{}, Warnings: []string{warning}}
	}
	if component.SupportGrade == "C" {
		return unsupported("Component support grade is C (unsupported mesh layout)")
	}
	if len(blendBytes) == 0 || blendStride == 0 {
		return unsupported("Component has no blend buffer for bone-based selection")
	}
	zones := make([]TouchZoneSpec, 0, len(selections))
	warnings := []string{}
	for _, selection := range selections {
		if selection.Channel == nil {
			continue
		}
		seeds := make([]int, 0)
		for vertex := 0; vertex < component.VertexCount && vertex*blendStride+blendStride <= len(blendBytes); vertex++ {
			weight := float32(0)
			visitBlendInfluences(blendBytes, vertex*blendStride, blendStride, func(id uint32, value float32) {
				if id == selection.BoneID && value > weight {
					weight = value
				}
			})
			if weight > 0 && float64(weight) >= threshold[0] && float64(weight) <= threshold[1] {
				seeds = append(seeds, vertex)
			}
		}
		minimum := min(12, max(3, int(math.Floor(float64(component.VertexCount)*.01))))
		if len(seeds) < minimum {
			warnings = append(warnings, fmt.Sprintf("Bone %d: only %d vertices within threshold %g-%g (need %d)", selection.BoneID, len(seeds), threshold[0], threshold[1], minimum))
			continue
		}
		center := [3]float64{}
		for _, vertex := range seeds {
			for axis := range 3 {
				center[axis] += float64(positions[vertex*3+axis])
			}
		}
		for axis := range 3 {
			center[axis] /= float64(len(seeds))
		}
		radius := [3]float64{}
		for _, vertex := range seeds {
			for axis := range 3 {
				radius[axis] = math.Max(radius[axis], math.Abs(float64(positions[vertex*3+axis])-center[axis]))
			}
		}
		for axis := range 3 {
			radius[axis] = math.Max(radius[axis]*1.05, .02)
		}
		label := fmt.Sprintf("Bone %d", selection.BoneID)
		if selection.Label != nil {
			label = *selection.Label
		}
		zones = append(zones, TouchZoneSpec{
			ID: fmt.Sprintf("bone_%d_ch%d", selection.BoneID, *selection.Channel), Label: label,
			Channel: *selection.Channel, Confidence: 1, Center: center, Radius: radius, Source: "bone",
			Settings: defaultTouchZoneSettings(), Seeds: seeds,
		})
	}
	if len(zones) == 0 {
		if len(warnings) == 0 {
			warnings = []string{"No zones produced from bone selection"}
		}
		return TouchComponentDraft{ComponentID: component.ID, ObjectID: objectID, Zones: zones, Warnings: warnings}
	}
	return TouchComponentDraft{ComponentID: component.ID, Interactive: true, ObjectID: objectID, Zones: zones, Confidence: 1, Warnings: warnings}
}
