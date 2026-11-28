package modelviewer

import (
	"fmt"
	"slices"
	"strings"
)

// detectModelViewerFormSelector finds the variable that picks one form, as the master INI of a namespace merge
// declares: every mesh of a form draws only at one value of it, each of its values draws a different form, and
// its default is one of them. The forms then follow its values in order. A value the INI names keeps its label
// and lends it to its form; the others take their form's label.
func detectModelViewerFormSelector(transport *ModelViewerTransport) {
	forms := transport.Forms
	if len(forms) < 2 || slices.ContainsFunc(forms, func(form ModelViewerForm) bool { return form.Group != "" }) {
		return
	}
	for index := range transport.Variables {
		variable := &transport.Variables[index]
		formOfValue := modelViewerFormsByValue(transport, *variable)
		if formOfValue == nil {
			continue
		}

		byID := make(map[string]ModelViewerForm, len(forms))
		for _, form := range forms {
			byID[form.ID] = form
		}
		ordered := make([]ModelViewerForm, 0, len(forms))
		labels := make([]string, 0, len(forms))
		for _, value := range variable.Values {
			form := byID[formOfValue[fmt.Sprint(value.Value)]]
			form.Value = value.Value
			ordered = append(ordered, form)
			labels = append(labels, form.Label)
		}
		labels = trimModelViewerCommonLabelPrefix(labels)
		for at, value := range variable.Values {
			if value.Label != "" && value.Label != fmt.Sprint(value.Value) {
				ordered[at].Label = value.Label
				continue
			}
			ordered[at].Label = labels[at]
			variable.Values[at].Label = labels[at]
		}
		transport.Forms = ordered
		transport.FormVariable = variable.ID
		return
	}
}

// modelViewerFormsByValue maps each value of variable to the form it alone draws, or returns nil when variable
// does not pick between the forms that way or its default draws none of them.
func modelViewerFormsByValue(transport *ModelViewerTransport, variable ModelViewerVariable) map[string]string {
	forms := transport.Forms
	if variable.ControlType == "slider" || len(variable.Values) != len(forms) ||
		slices.ContainsFunc(forms, func(form ModelViewerForm) bool {
			return form.ID == variable.Form || strings.HasPrefix(form.ID, variable.Form+"::")
		}) {
		return nil
	}

	valueOfForm := make(map[string]string, len(forms))
	for _, mesh := range transport.Meshes {
		if !slices.ContainsFunc(forms, func(form ModelViewerForm) bool { return form.ID == mesh.Form }) {
			continue
		}
		value, ok := modelViewerDNFRequiredValue(mesh.Conditions, variable.ID)
		if !ok {
			return nil
		}
		if seen, found := valueOfForm[mesh.Form]; found && seen != value {
			return nil
		}
		valueOfForm[mesh.Form] = value
	}
	if len(valueOfForm) != len(forms) {
		return nil
	}

	formOfValue := make(map[string]string, len(forms))
	for form, value := range valueOfForm {
		formOfValue[value] = form
	}
	if len(formOfValue) != len(forms) {
		return nil
	}
	for _, value := range variable.Values {
		if _, ok := formOfValue[fmt.Sprint(value.Value)]; !ok {
			return nil
		}
	}
	defaultValue, set := transport.DefaultState[variable.ID]
	if !set {
		defaultValue = variable.DefaultValue
	}
	if _, ok := formOfValue[fmt.Sprint(defaultValue)]; !ok {
		return nil
	}
	return formOfValue
}

// modelViewerDNFRequiredValue returns the value variable must equal for dnf to hold, when every clause list
// requires the same one.
func modelViewerDNFRequiredValue(dnf ModelViewerDNF, variable string) (string, bool) {
	required := ""
	for _, clauses := range dnf {
		value := ""
		for _, clause := range clauses {
			if clause.Var != variable || clause.Negate {
				continue
			}
			if value != "" && value != clause.Value {
				return "", false
			}
			value = clause.Value
		}
		if value == "" || (required != "" && required != value) {
			return "", false
		}
		required = value
	}
	return required, required != ""
}

// trimModelViewerCommonLabelPrefix drops the leading words all labels share, such as the character name the
// folders of a merge repeat. Words end at '_', '-' or a space; labels stay whole when one would be left empty or
// two would read the same.
func trimModelViewerCommonLabelPrefix(labels []string) []string {
	if len(labels) < 2 {
		return labels
	}
	prefix := labels[0]
	for _, label := range labels[1:] {
		for !strings.HasPrefix(label, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	cut := strings.LastIndexAny(prefix, "_- ") + 1
	if cut == 0 {
		return labels
	}
	trimmed := make([]string, len(labels))
	seen := make(map[string]bool, len(labels))
	for at, label := range labels {
		rest := strings.TrimLeft(label[cut:], "_- ")
		if rest == "" || seen[rest] {
			return labels
		}
		seen[rest] = true
		trimmed[at] = rest
	}
	return trimmed
}
