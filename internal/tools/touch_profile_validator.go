package tools

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type touchSettingsFingerprint struct{ Radius, Strength, Falloff, MaxOffset, Damping, Spring float64 }

func validateTouchOutput(outputRoot, iniPath string, components []TouchComponentAnalysis, drafts []TouchComponentDraft, assets []TouchGeneratedAssets) (TouchValidationResult, error) {
	result := TouchValidationResult{OK: true, Issues: []TouchValidationIssue{}}
	add := func(level, code, message string, componentID *string) {
		result.Issues = append(result.Issues, TouchValidationIssue{Level: level, Code: code, Message: message, ComponentID: componentID})
		if level == "error" {
			result.OK = false
		}
	}
	iniRaw, err := os.ReadFile(iniPath)
	if err != nil {
		return result, err
	}
	iniText := string(iniRaw)
	for _, shader := range touchShaderFiles {
		if info, statErr := os.Stat(filepath.Join(outputRoot, "Resources", "IM", shader)); statErr != nil || !info.Mode().IsRegular() {
			add("error", "missing_shader", "Missing runtime shader: "+shader, nil)
		}
	}
	headers := regexp.MustCompile(`(?m)^\s*\[([^\]]+)\]\s*$`).FindAllStringSubmatch(iniText, -1)
	seen, duplicates := map[string]bool{}, map[string]bool{}
	for _, match := range headers {
		header := strings.ToLower(strings.TrimSpace(match[1]))
		if seen[header] {
			duplicates[header] = true
		}
		seen[header] = true
	}
	if len(duplicates) > 0 {
		names := make([]string, 0, len(duplicates))
		for name := range duplicates {
			names = append(names, name)
		}
		add("warning", "duplicate_ini_section", "Duplicate INI sections preserved: "+strings.Join(names, ", "), nil)
	}
	channels := map[int]touchSettingsFingerprint{}
	for _, draft := range drafts {
		if !draft.Interactive {
			continue
		}
		for _, zone := range draft.Zones {
			settings, settingsErr := normalizeTouchZoneSettings(zone.Settings)
			if settingsErr != nil {
				id := draft.ComponentID
				add("error", "invalid_zone_settings", fmt.Sprintf("Invalid settings for %s: %s", zone.ID, settingsErr), &id)
				continue
			}
			params := resolveTouchJiggleParams(settings, draft.ObjectID)
			next := touchSettingsFingerprint{params.Radius, params.Strength, params.Falloff, params.MaxOffset, params.GrabDamping / defaultTouchJiggleParams.GrabDamping, params.GrabSpring / defaultTouchJiggleParams.GrabSpring}
			if previous, ok := channels[zone.Channel]; ok && !sameTouchFingerprint(previous, next) {
				id := draft.ComponentID
				add("error", "conflicting_zone_channel", fmt.Sprintf("Conflicting settings for runtime zone channel %d", zone.Channel), &id)
			} else {
				channels[zone.Channel] = next
			}
		}
	}
	for _, asset := range assets {
		var component *TouchComponentAnalysis
		for i := range components {
			if components[i].ID == asset.ComponentID {
				component = &components[i]
				break
			}
		}
		var draft *TouchComponentDraft
		for i := range drafts {
			if drafts[i].ComponentID == asset.ComponentID {
				draft = &drafts[i]
				break
			}
		}
		if component == nil || draft == nil || !draft.Interactive {
			continue
		}
		id := component.ID
		nonzero := 0
		for _, relative := range asset.MaskPaths {
			absolute := filepath.Join(outputRoot, filepath.FromSlash(relative))
			raw, readErr := os.ReadFile(absolute)
			if readErr != nil {
				add("error", "missing_mask", "Missing mask file: "+relative, &id)
				continue
			}
			expected := component.VertexCount * 16
			if len(raw) != expected {
				add("error", "mask_size", fmt.Sprintf("Mask size mismatch for %s: %d != %d", relative, len(raw), expected), &id)
			}
			for offset := 0; offset+4 <= len(raw); offset += 4 {
				value := math.Float32frombits(binary.LittleEndian.Uint32(raw[offset:]))
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 || value > 1 {
					add("error", "mask_value", "Mask contains invalid value in "+relative, &id)
					break
				}
				if value > 0 {
					nonzero++
				}
			}
		}
		if nonzero == 0 {
			add("error", "empty_mask", "No active mask weights across Masks0/1/2 for "+component.Name, &id)
		}
		for _, objectMap := range asset.ObjectMapPaths {
			raw, readErr := os.ReadFile(objectMap.AbsolutePath)
			if readErr != nil {
				add("error", "missing_object_map", "Missing ObjectMap: "+objectMap.RelativePath, &id)
				continue
			}
			if len(raw) < 32 || len(raw)%16 != 0 {
				add("error", "object_map_size", "Invalid ObjectMap size: "+objectMap.RelativePath, &id)
			}
		}
		if info, statErr := os.Stat(asset.ParamsAbsolutePath); statErr != nil || !info.Mode().IsRegular() {
			add("error", "missing_params", "Missing params: "+asset.ParamsRelativePath, &id)
		} else if info.Size() != 64 {
			add("error", "params_size", "Params size must be 64 bytes: "+asset.ParamsRelativePath, &id)
		}
		for _, snippet := range []string{"rzm_jiggle_interaction.hlsl", "rzm_object_detect.hlsl", fmt.Sprintf("dispatch = (%d + 255) // 256, 1, 1", component.VertexCount)} {
			if !strings.Contains(iniText, snippet) {
				add("error", "ini_missing_snippet", "INI missing required snippet: "+snippet, &id)
			}
		}
	}
	return result, nil
}

func sameTouchFingerprint(left, right touchSettingsFingerprint) bool {
	return mathAbs(left.Radius-right.Radius) < 1e-9 && mathAbs(left.Strength-right.Strength) < 1e-9 && mathAbs(left.Falloff-right.Falloff) < 1e-9 && mathAbs(left.MaxOffset-right.MaxOffset) < 1e-9 && mathAbs(left.Damping-right.Damping) < 1e-9 && mathAbs(left.Spring-right.Spring) < 1e-9
}
