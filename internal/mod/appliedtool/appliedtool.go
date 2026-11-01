// Package appliedtool holds the traces mod tools leave in a mod's root folder
// and detects them. The tool packages write these traces and the mod scanner
// reads them; neither depends on the other.
package appliedtool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BodyShape     = "body-shape"
	TouchProfile  = "touch-profile"
	LiquidEmitter = "liquid-emitter"

	BodyShapeChangeLogFile   = "변경사항.txt"
	BodyShapeChangeLogHeader = "[체형 수정 내역]"
	TouchProfileManifestFile = ".nahida-touch-profile.json"
	TouchProfileManifestKind = "nahida-touch-profile"
	LiquidMarkerFile         = ".nahida-liquid.json"
)

// Detect returns the tools whose output is in modPath. A copy made by another
// tool keeps the earlier tool's edits, so each trace counts on its own. Unlike
// the tools' own inspections, it reads only these root files, which keeps it
// cheap enough for every mod of a list.
func Detect(modPath string) ([]string, error) {
	var tools []string
	var errs []error
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(modPath, name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("read %s of %s: %w", name, modPath, err))
		}
		return raw
	}

	// The file name is generic, so only the header identifies the tool's log.
	if strings.HasPrefix(string(read(BodyShapeChangeLogFile)), BodyShapeChangeLogHeader) {
		tools = append(tools, BodyShape)
	}

	var manifest struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(read(TouchProfileManifestFile), &manifest) == nil && manifest.Kind == TouchProfileManifestKind {
		tools = append(tools, TouchProfile)
	}

	var marker struct {
		ModKey string `json:"modKey"`
		Empty  bool   `json:"empty"`
	}
	if json.Unmarshal(read(LiquidMarkerFile), &marker) == nil && marker.ModKey != "" && !marker.Empty {
		tools = append(tools, LiquidEmitter)
	}
	return tools, errors.Join(errs...)
}
