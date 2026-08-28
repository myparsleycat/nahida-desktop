package mod

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type MergePackClassification struct {
	Path           string   `json:"path"`
	Name           string   `json:"name"`
	Family         string   `json:"family"`
	Dialect        string   `json:"dialect"`
	PrimaryIniPath *string  `json:"primaryIniPath"`
	Hashes         []string `json:"hashes"`
	ObjectGuid     *string  `json:"objectGuid"`
	AllowsClassic  bool     `json:"allowsClassic"`
	Warnings       []string `json:"warnings"`
}

type ClassifyMergePacksResult struct {
	Packs       []MergePackClassification `json:"packs"`
	HashOverlap bool                      `json:"hashOverlap"`
	Warnings    []string                  `json:"warnings"`
}

var (
	hashLineRE        = regexp.MustCompile(`(?im)^\s*hash\s*=\s*([0-9a-f]+)\s*$`)
	objectGUIDRE      = regexp.MustCompile(`(?im)^\s*global\s+\$object_guid\s*=\s*(\S+)`)
	namespaceRE       = regexp.MustCompile(`(?im)^\s*namespace\s*=\s*([^;\r\n]+)`)
	keySectionRE      = regexp.MustCompile(`(?im)^\s*\[Key`)
	persistVarRE      = regexp.MustCompile(`(?im)^\s*global\s+persist\s+\$`)
	controlFlowRE     = regexp.MustCompile(`(?im)^\s*(?:if|else\s+if|elif|else|endif)\b`)
	numberedResRE     = regexp.MustCompile(`(?im)^\s*\[Resource[^\]]*\.\d+\]`)
	commandListRE     = regexp.MustCompile(`(?im)^\s*\[CommandList`)
	textureOverrideRE = regexp.MustCompile(`(?im)^\s*\[TextureOverride`)
	resourceRE        = regexp.MustCompile(`(?im)^\s*\[Resource`)
	typeCycleRE       = regexp.MustCompile(`(?im)^\s*type\s*=\s*cycle\s*$`)
)

func (m *Mod) ClassifyMergePacks(
	ctx context.Context,
	modPaths []string,
) (ClassifyMergePacksResult, error) {
	result := ClassifyMergePacksResult{Packs: []MergePackClassification{}, HashOverlap: true, Warnings: []string{}}
	if len(modPaths) == 0 {
		return result, errorsNewInvalidMergePacks()
	}
	for _, path := range modPaths {
		if !filepath.IsAbs(path) {
			return result, errorsNewInvalidMergePacks()
		}
		if _, err := m.ownedPath(ctx, path); err != nil {
			return result, err
		}
		classification, err := classifyMergePack(path)
		if err != nil {
			return result, err
		}
		result.Packs = append(result.Packs, classification)
	}
	comparable := []MergePackClassification{}
	warningSet := map[string]struct{}{}
	for _, pack := range result.Packs {
		for _, warning := range pack.Warnings {
			if _, exists := warningSet[warning]; !exists {
				warningSet[warning] = struct{}{}
				result.Warnings = append(result.Warnings, warning)
			}
		}
		if pack.Family != "support" && len(pack.Hashes) > 0 {
			comparable = append(comparable, pack)
		}
	}
	if len(comparable) >= 2 {
		for _, pack := range comparable[1:] {
			if !mergePacksOverlap(comparable[0], pack) {
				result.HashOverlap = false
				if _, exists := warningSet["hash_mismatch"]; !exists {
					warningSet["hash_mismatch"] = struct{}{}
					result.Warnings = append(result.Warnings, "hash_mismatch")
				}
				break
			}
		}
	}
	return result, nil
}

func classifyMergePack(modPath string) (MergePackClassification, error) {
	result := MergePackClassification{
		Path: modPath, Name: stripDisabled(filepath.Base(modPath)), Family: "support",
		Dialect: "unknown", Hashes: []string{}, Warnings: []string{},
	}
	inis, err := collectEnabledINIs(modPath)
	if err != nil {
		return result, err
	}
	if len(inis) == 0 {
		result.Warnings = []string{"no_enabled_ini"}
		return result, nil
	}
	type scoredINI struct {
		path  string
		text  string
		score int
	}
	scored := make([]scoredINI, 0, len(inis))
	allText := strings.Builder{}
	for _, path := range inis {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return result, readErr
		}
		text := string(raw)
		allText.WriteString(text)
		allText.WriteByte('\n')
		scored = append(scored, scoredINI{path: path, text: text, score: scoreMergeINI(path, text)})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].path < scored[j].path
	})
	primary := scored[0]
	result.PrimaryIniPath = stringPointer(primary.path)
	result.Dialect = detectMergeDialect(primary.text)
	result.Family = detectMergeFamily(primary.path, primary.text, allText.String())
	result.Hashes = extractMergeHashes(primary.text)
	result.ObjectGuid = regexFirst(objectGUIDRE, primary.text)
	result.Warnings = mergeWarnings(result.Family, result.Dialect, primary.text)
	result.AllowsClassic = result.Family == "ordinary" && result.Dialect != "wwmi" &&
		result.Dialect != "efmi" && result.Dialect != "unknown" && !controlFlowRE.MatchString(primary.text)
	return result, nil
}

func collectEnabledINIs(root string) ([]string, error) {
	result := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if (strings.HasPrefix(name, "disabled") && strings.HasSuffix(name, ".ini")) ||
			strings.HasPrefix(name, "disabled_backup_") || isSupportININame(name) {
			return nil
		}
		relative, _ := filepath.Rel(root, filepath.Dir(path))
		for _, segment := range strings.FieldsFunc(filepath.ToSlash(relative), func(r rune) bool { return r == '/' }) {
			if isDisabled(segment) {
				return nil
			}
		}
		result = append(result, path)
		return nil
	})
	sort.Strings(result)
	return result, err
}

func detectMergeDialect(text string) string {
	switch {
	case regexAny(text, `(?i);\s*WWMI`, `(?i)\$\\WWMIv1\\`, `(?i)\[TextureOverrideComponent\d`):
		return "wwmi"
	case regexAny(text, `(?i);\s*EFMI`, `(?i)\$\\EFMIv1\\`, `(?i)\[TextureOverride_Component\d`):
		return "efmi"
	case regexAny(text, `(?i)\$\\SRMI\\`, `(?i)Resource\\SRMI\\`, `(?i)HeadBlend|HairBlend`):
		return "srmi"
	case regexAny(text, `(?i)\$\\ZZMI\\`, `(?i)Resource\\ZZMI\\`):
		return "zzmi"
	case regexAny(text, `(?i)TextureOverride\w+Position`, `(?i)VertexLimitRaise`):
		return "gimi"
	default:
		return "unknown"
	}
}

func detectMergeFamily(primaryPath, primaryText, packText string) string {
	base := strings.ToLower(filepath.Base(primaryPath))
	namespace := regexFirst(namespaceRE, primaryText)
	masterName := strings.HasPrefix(base, "master") && strings.HasSuffix(base, ".ini")
	if masterName || (namespace != nil && strings.HasSuffix(strings.ToLower(*namespace), `\master`) &&
		!resourceRE.MatchString(primaryText)) || masterSwapRefRE.MatchString(packText) {
		return "namespace_merge"
	}
	if hasMergedModHeader(primaryText) || base == "merged.ini" ||
		(numberedResRE.MatchString(primaryText) && strings.Contains(strings.ToLower(primaryText), "$swapvar") &&
			commandListRE.MatchString(primaryText)) {
		return "classic_merge"
	}
	if isSupportINIText(primaryText, base) {
		return "support"
	}
	if keySectionRE.MatchString(primaryText) || len(persistVarRE.FindAllString(primaryText, -1)) >= 2 {
		return "in_mod_toggle"
	}
	return "ordinary"
}

func mergeWarnings(family, dialect, text string) []string {
	result := []string{}
	if family == "in_mod_toggle" {
		result = append(result, "in_mod_toggle")
	}
	if family == "support" {
		result = append(result, "support")
	}
	if dialect == "wwmi" || dialect == "efmi" {
		result = append(result, "namespace_only_dialect")
	}
	if dialect == "unknown" {
		result = append(result, "unknown_dialect")
	}
	if namespaceRE.MatchString(text) && family == "in_mod_toggle" {
		result = append(result, "namespaced_toggle")
	}
	return result
}

func scoreMergeINI(path, text string) int {
	base := strings.ToLower(filepath.Base(path))
	score := 0
	if base == "merged.ini" {
		score += 120
	}
	if strings.HasPrefix(base, "master") && strings.HasSuffix(base, ".ini") {
		score += 140
	}
	if hasMergedModHeader(text) {
		score += 80
	}
	if namespaceRE.MatchString(text) {
		score += 60
	}
	score += len(persistVarRE.FindAllString(text, -1)) * 15
	score += len(typeCycleRE.FindAllString(text, -1)) * 10
	score += min(len(textureOverrideRE.FindAllString(text, -1)), 50)
	score += min(len(resourceRE.FindAllString(text, -1)), 50)
	if regexAny(text, `(?im)^\s*\[KeyHelp\]`) {
		score -= 25
	}
	if strings.HasPrefix(base, "disabled") && !hasMergedModHeader(text) {
		score -= 10
	}
	return score
}

func extractMergeHashes(text string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, match := range hashLineRE.FindAllStringSubmatch(text, -1) {
		value := strings.ToLower(match[1])
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func mergePacksOverlap(left, right MergePackClassification) bool {
	if left.ObjectGuid != nil && right.ObjectGuid != nil {
		return *left.ObjectGuid == *right.ObjectGuid
	}
	set := map[string]struct{}{}
	for _, hash := range right.Hashes {
		set[hash] = struct{}{}
	}
	for _, hash := range left.Hashes {
		if _, ok := set[hash]; ok {
			return true
		}
	}
	return false
}

func hasMergedModHeader(text string) bool {
	return len(extractMergedModPaths(text)) > 0
}

func isSupportININame(name string) bool {
	switch strings.ToLower(name) {
	case "crossibclassifier.ini", "ibskip.ini", "draw_image.ini", "cutoutmask.ini",
		"menu.ini", "help.ini", "selectionmenu.ini", "qh.ini":
		return true
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "preset") && strings.HasSuffix(lower, ".ini")
}

func isSupportINIText(text, basename string) bool {
	if isSupportININame(basename) {
		return true
	}
	supportMarker := regexAny(text,
		`(?i)CommandList\\global\\ORFix`, `(?i)CommandList\\TexFx`,
		`(?i)Resource\\ZZMI\\`, `(?i)Resource\\SRMI\\`, `(?i)Resource\\WWMIv1\\`,
		`(?i)Resource\\EFMIv1\\`, `(?i)ShaderFixes\\help`)
	return supportMarker && !textureOverrideRE.MatchString(text)
}

func regexFirst(expression *regexp.Regexp, text string) *string {
	match := expression.FindStringSubmatch(text)
	if len(match) < 2 {
		return nil
	}
	value := strings.TrimSpace(match[1])
	return &value
}

func regexAny(text string, expressions ...string) bool {
	for _, expression := range expressions {
		if regexp.MustCompile(expression).MatchString(text) {
			return true
		}
	}
	return false
}

func errorsNewInvalidMergePacks() error {
	return &mergeValidationError{message: "Invalid merge pack payload"}
}

type mergeValidationError struct{ message string }

func (e *mergeValidationError) Error() string { return e.message }
