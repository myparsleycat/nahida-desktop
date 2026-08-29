package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type touchInputDetection struct {
	Status    string
	Reasons   []string
	Namespace string
}

func assertTouchProfileInputAllowed(modPath string) error {
	_, _, sourcePaths, err := loadModINIBundleWithSources(modPath)
	if err != nil {
		return err
	}
	iniPath, err := findPrimaryModINI(modPath)
	if err != nil {
		return err
	}
	return assertTouchProfileBundleAllowed(filepath.Dir(iniPath), sourcePaths)
}

func assertTouchProfileBundleAllowed(root string, sourcePaths []string) error {
	detection, err := inspectTouchProfileBundle(root, sourcePaths)
	if err != nil {
		return err
	}
	if detection.Status == "none" {
		return nil
	}
	absolute, _ := filepath.Abs(root)
	code, message := "TOUCH_PROFILE_INPUT_ALREADY_TOUCH", "Input is already a Nahida Touch Profile mod and cannot be converted again."
	if detection.Status == "suspected" {
		code, message = "TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH", "Input appears to be an existing Touch mod and cannot be safely converted."
	}
	details := ""
	if len(detection.Reasons) > 0 {
		details = " " + strings.Join(detection.Reasons, "; ") + "."
	}
	return contractError(fmt.Sprintf("%s: %s%s Path: %s", code, message, details, absolute))
}

func inspectTouchProfileBundle(root string, sourcePaths []string) (touchInputDetection, error) {
	root, _ = filepath.Abs(root)
	if raw, err := os.ReadFile(filepath.Join(root, touchProfileManifestFile)); err == nil {
		var manifest struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &manifest) == nil && manifest.Kind == touchProfileManifestKind {
			return touchDetectionWithFiles(root, touchInputDetection{Status: "generated", Reasons: []string{"Nahida Touch Profile manifest found"}}), nil
		}
	}
	var builder strings.Builder
	for _, path := range sourcePaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return touchInputDetection{}, err
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	text := builder.String()
	states := touchMarkerNamespaces(text, regexp.MustCompile(`(?i)Nahida Touch Profile state\s*\(\s*([^\s)]+)\s*\)`))
	runtimes := touchMarkerNamespaces(text, regexp.MustCompile(`(?i)Nahida Touch Profile runtime\s*\(\s*([^\s)]+)\s*\)`))
	for _, state := range states {
		for _, runtime := range runtimes {
			if state == runtime {
				return touchDetectionWithFiles(root, touchInputDetection{Status: "generated", Reasons: []string{"Nahida Touch Profile INI markers found"}, Namespace: state}), nil
			}
		}
	}
	if len(states) > 0 || len(runtimes) > 0 {
		return touchDetectionWithFiles(root, touchInputDetection{Status: "incomplete", Reasons: []string{"Incomplete Nahida Touch Profile INI markers found"}}), nil
	}
	missing := missingTouchShaders(root)
	lower := strings.ToLower(text)
	references := true
	for _, name := range []string{"rzm_gs_probe.hlsl", "rzm_object_detect.hlsl", "rzm_jiggle_interaction.hlsl"} {
		references = references && strings.Contains(lower, name)
	}
	if len(missing) == 0 && references {
		return touchInputDetection{Status: "suspected", Reasons: []string{"Touch runtime shaders and INI references found"}}, nil
	}
	return touchInputDetection{Status: "none", Reasons: []string{}}, nil
}

func touchDetectionWithFiles(root string, input touchInputDetection) touchInputDetection {
	missing := missingTouchShaders(root)
	if len(missing) > 0 {
		input.Status = "incomplete"
		input.Reasons = append(input.Reasons, "Missing runtime shaders: "+strings.Join(missing, ", "))
	}
	return input
}
func missingTouchShaders(root string) []string {
	missing := []string{}
	for _, name := range touchShaderFiles {
		if info, err := os.Stat(filepath.Join(root, "Resources", "IM", name)); err != nil || !info.Mode().IsRegular() {
			missing = append(missing, name)
		}
	}
	return missing
}
func touchMarkerNamespaces(text string, re *regexp.Regexp) []string {
	matches := re.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, strings.ToLower(match[1]))
	}
	return out
}
