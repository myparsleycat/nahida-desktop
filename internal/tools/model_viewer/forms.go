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
	group := filepath.Base(filepath.Dir(iniPath))
	for _, label := range labels {
		id := parent + "::" + filepath.Base(iniPath) + "::" + label
		p.forms = append(p.forms, ModelViewerForm{ID: id, Label: label, Group: group})
		for index := range meshes {
			if strings.EqualFold(meshes[index].commandForm, label) {
				meshes[index].form = id
			}
		}
	}
}

// qualifyModelViewerFormGroups replaces a folder-name heading shared by the
// groups of several INIs, such as two INIs of one folder or two folders with
// the same name, with each group's INI path.
func qualifyModelViewerFormGroups(forms []ModelViewerForm) {
	iniPath := func(form ModelViewerForm) string {
		source := form.ID[:strings.LastIndex(form.ID, "::")]
		return strings.TrimPrefix(strings.Replace(source, "::", "/", 1), "./")
	}
	sources := make(map[string]map[string]bool)
	for _, form := range forms {
		if form.Group == "" {
			continue
		}
		if sources[form.Group] == nil {
			sources[form.Group] = make(map[string]bool)
		}
		sources[form.Group][iniPath(form)] = true
	}
	for index := range forms {
		if len(sources[forms[index].Group]) > 1 {
			forms[index].Group = iniPath(forms[index])
		}
	}
}
