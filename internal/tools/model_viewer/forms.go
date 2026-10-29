package modelviewer

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Some exporters wrap form-specific draws in CommandList..._<form>Sub and
// leave shared draws outside those wrappers. A single label is insufficient
// evidence of multiple forms, and ordinary ...BaseSub lists remain shared.
var modelViewerCommandFormRE = regexp.MustCompile(`(?i)_([a-z][a-z0-9]*)Sub$`)

func (p *modelViewerPreparedGeometry) scopeCommandForms(
	meshes []modelViewerDirectMesh,
	iniPath string,
	scope modelViewerINIScope,
) {
	var labels []string
	for _, mesh := range meshes {
		if mesh.commandForm != "" && !slices.ContainsFunc(labels, func(label string) bool {
			return strings.EqualFold(label, mesh.commandForm)
		}) {
			labels = append(labels, mesh.commandForm)
		}
	}
	if len(labels) < 2 {
		return
	}

	parent := scope.form
	if parent == "" {
		parent = "."
	}
	for _, label := range labels {
		id := parent + "::" + filepath.Base(iniPath) + "::" + label
		p.forms = append(p.forms, ModelViewerForm{ID: id, Label: label})
		for index := range meshes {
			if strings.EqualFold(meshes[index].commandForm, label) {
				meshes[index].form = id
			}
		}
	}
}
