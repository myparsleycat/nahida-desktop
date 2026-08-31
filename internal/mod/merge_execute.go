package mod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxMergePlanDepth = 32
	maxMergePlanNodes = 256

	invalidMergeRequestMessage = "Invalid merge request payload"
	outsideManagedModsMessage  = "Path is outside the managed mod folder"
	outsideMergeGroupMessage   = "Path is outside the selected group"
)

type MergePlanNode struct {
	Kind           string          `json:"kind"`
	Path           string          `json:"path,omitempty"`
	ID             string          `json:"id,omitempty"`
	Engine         string          `json:"engine,omitempty"`
	Name           string          `json:"name,omitempty"`
	ForwardKey     string          `json:"forwardKey,omitempty"`
	BackKey        string          `json:"backKey,omitempty"`
	IncludeVanilla bool            `json:"includeVanilla,omitempty"`
	Children       []MergePlanNode `json:"children,omitempty"`
}

type MergeModsRequest struct {
	GroupPath string        `json:"groupPath"`
	Placement string        `json:"placement"`
	PackName  string        `json:"packName"`
	Root      MergePlanNode `json:"root"`
}

type MergeModsResult struct {
	OutputPath string `json:"outputPath"`
}

type mergeRollback struct {
	kind    string
	path    string
	from    string
	to      string
	content []byte
	mode    os.FileMode
}

type mergeRollbackFailure struct {
	action mergeRollback
	err    error
}

type mergeRollbackFailureLog struct {
	Action string `json:"action"`
	Error  string `json:"error"`
}

type mergeFailureLog struct {
	Operation        string                    `json:"operation"`
	GroupPath        string                    `json:"groupPath"`
	Placement        string                    `json:"placement"`
	PackName         string                    `json:"packName"`
	Stage            string                    `json:"stage"`
	Created          []string                  `json:"created"`
	RollbackFailures []mergeRollbackFailureLog `json:"rollbackFailures"`
	Error            string                    `json:"error"`
}

var (
	rollbackRemovePath = os.RemoveAll
	rollbackMovePath   = movePathOverwrite
	rollbackWriteFile  = os.WriteFile
)

type classicEntry struct {
	control bool
	key     string
	value   string
	raw     string
}

type classicSection struct {
	header     string
	name       string
	entries    []classicEntry
	location   string
	groupIndex int
}

func (m *Mod) MergeMods(ctx context.Context, request MergeModsRequest) (result MergeModsResult, err error) {
	if err := m.validateMergeRequest(ctx, request); err != nil {
		return result, err
	}
	created := []mergeRollback{}
	defer func() {
		if err != nil {
			failures := rollbackMerge(created)
			m.logMergeFailure(request, created, failures, err)
		}
	}()
	output, err := m.executeMergeNode(ctx, request.Root, request, &created)
	if err != nil {
		return result, err
	}
	return MergeModsResult{OutputPath: output}, nil
}

func (m *Mod) validateMergeRequest(ctx context.Context, request MergeModsRequest) error {
	if !filepath.IsAbs(request.GroupPath) ||
		(request.Placement != "in_place" && request.Placement != "new_folder") ||
		(strings.TrimSpace(request.PackName) != "" && !safeMergeName(request.PackName)) ||
		request.Root.Kind != "group" {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	if _, err := m.ownedPath(ctx, request.GroupPath); err != nil {
		return mergeOwnedPathError(err)
	}
	count := 0
	leaves := []string{}
	if err := validateMergeNode(request.Root, 0, &count, &leaves); err != nil {
		return err
	}
	if len(leaves) < 2 {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	if !uniqueLexicalMergeLeaves(leaves) {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	group, err := resolveForCompare(request.GroupPath)
	if err != nil {
		return err
	}
	for _, leaf := range leaves {
		if _, err := m.ownedPath(ctx, leaf); err != nil {
			return mergeOwnedPathError(err)
		}
		if err := m.rejectActiveDownloadAction(leaf); err != nil {
			return err
		}
		resolved, err := resolveForCompare(leaf)
		if err != nil {
			return err
		}
		if !strictChildPath(group, resolved) {
			return &mergeValidationError{message: outsideMergeGroupMessage}
		}
	}
	return validateMergeOutputs(request.Root, group, request.PackName)
}

func uniqueLexicalMergeLeaves(leaves []string) bool {
	seen := make(map[string]struct{}, len(leaves))
	for _, leaf := range leaves {
		key := strings.ToLower(filepath.Clean(leaf))
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validateMergeNode(
	node MergePlanNode,
	depth int,
	count *int,
	leaves *[]string,
) error {
	*count++
	if depth > maxMergePlanDepth || *count > maxMergePlanNodes {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	if node.Kind == "leaf" {
		if !filepath.IsAbs(node.Path) {
			return &mergeValidationError{message: invalidMergeRequestMessage}
		}
		*leaves = append(*leaves, node.Path)
		return nil
	}
	if node.Kind != "group" || strings.TrimSpace(node.ID) == "" ||
		(node.Engine != "classic" && node.Engine != "namespace") || !safeMergeName(node.Name) ||
		strings.TrimSpace(node.ForwardKey) == "" || strings.ContainsAny(node.ForwardKey, "\r\n") ||
		strings.ContainsAny(node.BackKey, "\r\n") ||
		(node.Engine == "namespace" && strings.TrimSpace(node.BackKey) == "") || len(node.Children) == 0 {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	before := len(*leaves)
	for _, child := range node.Children {
		if err := validateMergeNode(child, depth+1, count, leaves); err != nil {
			return err
		}
	}
	if len(*leaves)-before < 2 {
		return &mergeValidationError{message: invalidMergeRequestMessage}
	}
	return nil
}

func validateMergeOutputs(node MergePlanNode, group, packName string) error {
	if node.Kind == "leaf" {
		return nil
	}
	name := strings.TrimSpace(node.Name)
	if name == "" {
		name = strings.TrimSpace(packName)
	}
	if name == "" {
		name = "Merged"
	}
	output, err := resolveForCompare(filepath.Join(group, name))
	if err != nil || !strictChildPath(group, output) {
		return &mergeValidationError{message: outsideMergeGroupMessage}
	}
	for _, child := range node.Children {
		if err := validateMergeOutputs(child, group, packName); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mod) executeMergeNode(
	ctx context.Context,
	node MergePlanNode,
	request MergeModsRequest,
	created *[]mergeRollback,
) (string, error) {
	if node.Kind == "leaf" {
		return node.Path, nil
	}
	children := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		path, err := m.executeMergeNode(ctx, child, request, created)
		if err != nil {
			return "", err
		}
		children = append(children, path)
	}
	packs := make([]MergePackClassification, len(children))
	for i, path := range children {
		classification, err := classifyMergePack(path)
		if err != nil {
			return "", err
		}
		packs[i] = classification
	}
	if node.Engine == "classic" {
		for _, pack := range packs {
			if !pack.AllowsClassic {
				return "", fmt.Errorf("CLASSIC_LOCKED:%s", pack.Path)
			}
		}
		return m.executeClassicMerge(node, packs, request, created)
	}
	return m.executeNamespaceMerge(node, packs, request, created)
}

func (m *Mod) executeClassicMerge(
	node MergePlanNode,
	packs []MergePackClassification,
	request MergeModsRequest,
	created *[]mergeRollback,
) (string, error) {
	container, err := createUniqueMergeFolder(request.GroupPath, mergeFolderName(node, request), created)
	if err != nil {
		return "", err
	}
	names, err := enabledMergeNames(packs, container)
	if err != nil {
		return "", err
	}
	mapped := make([]string, len(packs))
	for i, pack := range packs {
		destination := filepath.Join(container, names[i])
		if request.Placement == "new_folder" {
			if err := copyDirectory(pack.Path, destination); err != nil {
				return "", err
			}
			*created = append(*created, mergeRollback{kind: "remove", path: destination})
		} else {
			if err := os.Rename(pack.Path, destination); err != nil {
				return "", err
			}
			*created = append(*created, mergeRollback{kind: "move", from: destination, to: pack.Path})
		}
		mapped[i] = destination
	}
	sources := make([]classicSource, 0, len(packs))
	for i, pack := range packs {
		ini := ""
		if pack.PrimaryIniPath != nil {
			relative, relErr := filepath.Rel(pack.Path, *pack.PrimaryIniPath)
			if relErr == nil && fileExists(filepath.Join(mapped[i], relative)) {
				ini = filepath.Join(mapped[i], relative)
			}
		}
		if ini == "" {
			inis, collectErr := collectEnabledINIs(mapped[i])
			if collectErr != nil {
				return "", collectErr
			}
			if len(inis) > 0 {
				ini = inis[0]
			}
		}
		if ini != "" {
			sources = append(sources, classicSource{path: ini, index: i})
		}
	}
	if len(sources) < 2 {
		return "", errors.New("CLASSIC_MERGE_NEEDS_TWO_INIS")
	}
	if err := writeClassicMergedINI(container, sources, node.ForwardKey, node.BackKey, created); err != nil {
		return "", err
	}
	if request.Placement == "new_folder" {
		for _, pack := range packs {
			if err := disableOriginalForMerge(pack.Path, created); err != nil {
				return "", err
			}
		}
	}
	return container, nil
}

type classicSource struct {
	path  string
	index int
}

func writeClassicMergedINI(
	outputDir string,
	sources []classicSource,
	forwardKey, backKey string,
	created *[]mergeRollback,
) error {
	sections := []classicSection{}
	for _, source := range sources {
		raw, err := os.ReadFile(source.path)
		if err != nil {
			return err
		}
		sections = append(sections, parseClassicSections(string(raw), filepath.Dir(source.path), source.index)...)
	}
	type commandGroup struct {
		key      string
		sections []classicSection
	}
	commandIndex := map[string]int{}
	commandGroups := []commandGroup{}
	overrides, resources := []string{}, []string{}
	for _, section := range sections {
		hash := classicEntryValue(section, "hash")
		if hash != "" {
			key := strings.ToLower(hash) + "::" + classicEntryValueDefault(section, "match_first_index", "-1")
			index, exists := commandIndex[key]
			if !exists {
				index = len(commandGroups)
				commandIndex[key] = index
				commandGroups = append(commandGroups, commandGroup{key: key})
				overrides = append(overrides, buildClassicOverride(section, forwardKey != ""))
			}
			commandGroups[index].sections = append(commandGroups[index].sections, section)
			continue
		}
		if section.header == "CommandList" {
			key := section.name + "::0"
			index, exists := commandIndex[key]
			if !exists {
				index = len(commandGroups)
				commandIndex[key] = index
				commandGroups = append(commandGroups, commandGroup{key: key})
			}
			commandGroups[index].sections = append(commandGroups[index].sections, section)
			continue
		}
		if classicHasEntry(section, "filename") || classicHasEntry(section, "type") {
			resources = append(resources, buildClassicResource(section, outputDir))
		}
	}
	commands := []string{}
	for _, group := range commandGroups {
		if len(group.sections) > 0 && !strings.Contains(group.sections[0].name, "VertexLimitRaise") {
			commands = append(commands, buildClassicCommandList(group.sections))
		}
	}
	values := make([]string, len(sources))
	paths := make([]string, len(sources))
	for i, source := range sources {
		values[i] = fmt.Sprint(source.index)
		paths[i] = mergeRelativePath(outputDir, source.path)
	}
	backLine := ""
	if backKey != "" {
		backLine = "back = " + backKey + "\n"
	}
	present := "[Present]\npost $active = 0\n"
	for _, section := range sections {
		if strings.EqualFold(section.name, "creditinfo") {
			present += "run = CommandListCreditInfo\n"
			break
		}
	}
	content := fmt.Sprintf("; Merged Mod: %s\n\n; Constants ---------------------------\n\n"+
		"[Constants]\nglobal persist $swapvar = 0\nglobal $active\nglobal $creditinfo = 0\n\n"+
		"[KeySwap]\ncondition = $active == 1\nkey = %s\n%stype = cycle\n$swapvar = %s\n"+
		"$creditinfo = 0\n\n%s\n; Shader ------------------------------\n\n"+
		"; Overrides ---------------------------\n\n%s\n; CommandList -------------------------\n\n%s\n"+
		"; Resources ---------------------------\n\n%s\n\n; .ini generated by Nahida Desktop mod merger\n",
		strings.Join(paths, ", "), forwardKey, backLine, strings.Join(values, ","),
		present, strings.Join(overrides, "\n"), strings.Join(commands, "\n"), strings.Join(resources, "\n"))
	output := filepath.Join(outputDir, "merged.ini")
	if err := recordMergeWrite(output, created); err != nil {
		return err
	}
	if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
		return err
	}
	for _, source := range sources {
		if fileExists(source.path) {
			if err := disableINIForMerge(source.path, created); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseClassicSections(text, location string, groupIndex int) []classicSection {
	recognized := []string{"TextureOverride", "ShaderOverride", "Resource", "Constants", "Present", "CommandList", "CustomShader"}
	result := []classicSection{}
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			current = -1
			for _, header := range recognized {
				if strings.HasPrefix(name, header) && !strings.Contains(name, "CommandListReflectionTexture") &&
					!strings.Contains(name, "CommandListOutline") {
					result = append(result, classicSection{
						header: header, name: strings.TrimPrefix(name, header),
						location: location, groupIndex: groupIndex,
					})
					current = len(result) - 1
					break
				}
			}
			continue
		}
		if current < 0 {
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(raw, " \t"), ";") {
			continue
		}
		if isControlFlowLine(trimmed) {
			result[current].entries = append(result[current].entries, classicEntry{control: true, raw: trimmed})
			continue
		}
		if at := strings.IndexByte(raw, '='); at >= 0 {
			key, value := strings.TrimSpace(raw[:at]), strings.TrimSpace(raw[at+1:])
			if !strings.Contains(key, "CharacterIB") && !strings.Contains(key, "ResourceRef") {
				result[current].entries = append(result[current].entries, classicEntry{key: key, value: value})
			}
		}
	}
	return result
}

func buildClassicOverride(section classicSection, setActive bool) string {
	lines := []string{"[" + section.header + section.name + "]", "hash = " + classicEntryValue(section, "hash")}
	if value := classicEntryValue(section, "match_first_index"); value != "" {
		lines = append(lines, "match_first_index = "+value)
	}
	preserve := map[string]bool{"allow_duplicate_hash": true, "filter_index": true, "match_priority": true,
		"cull": true, "topology": true, "override_vertex_count": true, "override_byte_stride": true}
	for _, entry := range section.entries {
		if !entry.control && preserve[strings.ToLower(entry.key)] {
			lines = append(lines, entry.key+" = "+entry.value)
		}
	}
	if !strings.Contains(section.name, "VertexLimitRaise") {
		lines = append(lines, "run = CommandList"+section.name)
	}
	if setActive && strings.Contains(section.name, "Position") {
		lines = append(lines, "$active = 1")
	}
	return strings.Join(append(lines, ""), "\n")
}

func buildClassicResource(section classicSection, outputDir string) string {
	lines := []string{fmt.Sprintf("[%s%s.%d]", section.header, section.name, section.groupIndex)}
	for _, entry := range section.entries {
		if entry.control {
			continue
		}
		value := entry.value
		if strings.EqualFold(entry.key, "filename") {
			raw := strings.Trim(strings.TrimSpace(value), "\"'")
			if !filepath.IsAbs(raw) {
				raw = filepath.Join(section.location, raw)
			}
			value = mergeRelativePath(outputDir, raw)
		}
		lines = append(lines, entry.key+" = "+value)
	}
	return strings.Join(append(lines, ""), "\n")
}

func buildClassicCommandList(group []classicSection) string {
	lines := []string{"[CommandList" + group[0].name + "]"}
	excluded := map[string]bool{"hash": true, "match_first_index": true, "allow_duplicate_hash": true,
		"override_vertex_count": true, "override_byte_stride": true}
	for index, section := range group {
		branch := "if"
		if index > 0 {
			branch = "else if"
		}
		lines = append(lines, fmt.Sprintf("%s $swapvar == %d", branch, section.groupIndex))
		indent := 1
		for _, entry := range section.entries {
			if entry.control {
				lower := strings.ToLower(entry.raw)
				if lower == "endif" {
					indent = max(1, indent-1)
				} else if strings.HasPrefix(lower, "else") || strings.HasPrefix(lower, "elif") {
					indent = max(1, indent-1)
					lines = append(lines, strings.Repeat("\t", indent)+entry.raw)
					indent++
					continue
				}
				lines = append(lines, strings.Repeat("\t", indent)+entry.raw)
				if strings.HasPrefix(lower, "if") {
					indent++
				}
				continue
			}
			if excluded[strings.ToLower(entry.key)] {
				continue
			}
			value := entry.value
			if isClassicResourceReference(entry.key, value) {
				value = appendClassicResourceSuffix(value, section.groupIndex)
			}
			lines = append(lines, strings.Repeat("\t", indent)+entry.key+" = "+value)
		}
	}
	return strings.Join(append(lines, "endif", ""), "\n")
}

func (m *Mod) executeNamespaceMerge(
	node MergePlanNode,
	packs []MergePackClassification,
	request MergeModsRequest,
	created *[]mergeRollback,
) (string, error) {
	workingPacks := packs
	if request.Placement == "in_place" {
		var err error
		workingPacks, err = enablePackFolders(packs, created)
		if err != nil {
			return "", err
		}
	}
	namespacePacks := make([]MergePackClassification, 0, len(workingPacks))
	for _, pack := range workingPacks {
		if pack.Family == "namespace_merge" {
			namespacePacks = append(namespacePacks, pack)
		}
	}
	host := workingPacks[0]
	if len(namespacePacks) > 0 {
		host = namespacePacks[0]
		for _, pack := range namespacePacks {
			if pack.PrimaryIniPath != nil && isMasterIniPath(*pack.PrimaryIniPath) {
				host = pack
				break
			}
		}
	}
	hostMasterPath := ""
	if host.PrimaryIniPath != nil && isMasterIniPath(*host.PrimaryIniPath) {
		hostMasterPath = *host.PrimaryIniPath
	}

	var masterDir string
	var iniRoots []string
	existingMasterPath := ""
	var disableRoots []string
	if request.Placement == "new_folder" {
		var err error
		masterDir, err = createUniqueMergeFolder(request.GroupPath, mergeFolderName(node, request), created)
		if err != nil {
			return "", err
		}
		names, err := enabledMergeNames(workingPacks, masterDir)
		if err != nil {
			return "", err
		}
		for i, pack := range workingPacks {
			destination := filepath.Join(masterDir, names[i])
			if err := copyDirectory(pack.Path, destination); err != nil {
				return "", err
			}
			*created = append(*created, mergeRollback{kind: "remove", path: destination})
			iniRoots = append(iniRoots, destination)
			if err := disableOriginalForMerge(pack.Path, created); err != nil {
				return "", err
			}
		}
		disableRoots = []string{masterDir}
	} else {
		if hostMasterPath != "" {
			masterDir = filepath.Dir(hostMasterPath)
		} else {
			masterDir = host.Path
		}
		for _, pack := range workingPacks {
			if hostMasterPath == "" || pack.Path != host.Path {
				iniRoots = append(iniRoots, pack.Path)
			}
		}
		existingMasterPath = hostMasterPath
		for _, pack := range workingPacks {
			disableRoots = append(disableRoots, pack.Path)
		}
	}

	name, err := sanitizeMergeToken(node.Name)
	if err != nil || name == "" {
		name, err = sanitizeMergeToken(request.PackName)
	}
	if err != nil {
		return "", err
	}

	var existingChildren []string
	if existingMasterPath != "" {
		existingChildren, err = collectNamespaceChildren(existingMasterPath)
		if err != nil {
			return "", err
		}
	}
	sources, err := buildNamespaceSources(iniRoots, existingChildren, node.IncludeVanilla)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", errors.New("NAMESPACE_MERGE_NEEDS_CHILD")
	}
	masterPath, err := writeNamespaceMerge(namespaceMergeOptions{
		masterDir:          masterDir,
		name:               name,
		sources:            sources,
		forwardKey:         node.ForwardKey,
		backKey:            node.BackKey,
		includeVanilla:     node.IncludeVanilla,
		existingMasterPath: existingMasterPath,
		created:            created,
	})
	if err != nil {
		return "", err
	}
	if err := disableForeignMasters(disableRoots, masterPath, created); err != nil {
		return "", err
	}
	return filepath.Dir(masterPath), nil
}

func wrapNamespaceHashes(text, name string, index int) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	output := make([]string, 0, len(lines)+8)
	wrapping := false
	var pending []string
	flush := func() {
		if !wrapping {
			return
		}
		block := pending
		pending = nil
		wrapping = false
		output = append(output, buildNamespaceWrappedBlock(block, name, index)...)
	}
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		lower := strings.ToLower(trimmed)
		isComment := strings.HasPrefix(trimmed, ";")
		isHeader := !isComment && strings.HasPrefix(trimmed, "[")
		isHash := !isComment && (strings.HasPrefix(lower, "hash =") || strings.HasPrefix(lower, "hash="))
		if wrapping && (isHeader || isHash) {
			flush()
		}
		if !wrapping && isHash {
			output = append(output, raw)
			wrapping = true
			continue
		}
		if wrapping {
			pending = append(pending, raw)
			continue
		}
		output = append(output, raw)
	}
	flush()
	return strings.TrimRight(strings.Join(output, "\n"), "\n") + "\n"
}

func buildNamespaceWrappedBlock(block []string, name string, baseIndex int) []string {
	var matchLines, commandLines []string
	for _, raw := range block {
		key, _, ok := mergeINIKeyValue(raw)
		if ok && strings.EqualFold(key, "match_priority") {
			continue
		}
		if ok && isOverrideMatchKey(key) {
			matchLines = append(matchLines, strings.TrimLeft(raw, " \t"))
			continue
		}
		commandLines = append(commandLines, raw)
	}
	out := []string{fmt.Sprintf("match_priority = %d", baseIndex)}
	out = append(out, matchLines...)
	out = append(out, fmt.Sprintf("if $\\%s\\Master\\swapvar==%d", name, baseIndex))
	return append(out, closeNamespaceWrap(commandLines)...)
}

func closeNamespaceWrap(block []string) []string {
	lastCode := -1
	next := make([]string, len(block))
	for index, line := range block {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") {
			next[index] = line
			continue
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "[") {
			lastCode = index
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "[") {
			next[index] = line
			continue
		}
		next[index] = "\t" + line
	}
	if lastCode >= 0 {
		out := make([]string, 0, len(next)+2)
		out = append(out, next[:lastCode+1]...)
		out = append(out, "endif", "")
		out = append(out, next[lastCode+1:]...)
		return out
	}
	return append(next, "endif", "")
}

func mergeINIKeyValue(raw string) (string, string, bool) {
	line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
	if line == "" || strings.HasPrefix(line, "[") {
		return "", "", false
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

var overrideMatchKeys = map[string]bool{
	"match_first_index": true, "match_index_count": true, "match_byte_width": true,
	"match_stride": true, "match_type": true, "match_usage": true, "match_format": true,
	"match_width": true, "match_height": true, "filter_index": true, "allow_duplicate_hash": true,
}

func isOverrideMatchKey(key string) bool {
	return overrideMatchKeys[strings.ToLower(key)]
}

func rollbackMerge(actions []mergeRollback) []mergeRollbackFailure {
	failures := make([]mergeRollbackFailure, 0)
	for i := len(actions) - 1; i >= 0; i-- {
		action := actions[i]
		var err error
		switch action.kind {
		case "remove":
			err = rollbackRemovePath(action.path)
		case "move":
			if _, statErr := os.Stat(action.from); statErr == nil {
				err = rollbackMovePath(action.from, action.to)
			} else if !os.IsNotExist(statErr) {
				err = statErr
			}
		case "restore":
			err = rollbackWriteFile(action.path, action.content, action.mode)
		default:
			err = fmt.Errorf("unknown merge rollback action %q", action.kind)
		}
		if err != nil {
			failures = append(failures, mergeRollbackFailure{action: action, err: err})
		}
	}
	return failures
}

func describeMergeRollback(action mergeRollback) string {
	switch action.kind {
	case "remove":
		return action.path
	case "restore":
		return "restore:" + action.path
	case "move":
		return action.from + "->" + action.to
	default:
		return action.kind + ":" + action.path
	}
}

func (m *Mod) logMergeFailure(
	request MergeModsRequest,
	actions []mergeRollback,
	failures []mergeRollbackFailure,
	err error,
) {
	if m == nil || m.log == nil || err == nil {
		return
	}
	created := make([]string, len(actions))
	for i, action := range actions {
		created[i] = describeMergeRollback(action)
	}
	rollbackFailures := make([]mergeRollbackFailureLog, len(failures))
	for i, failure := range failures {
		rollbackFailures[i] = mergeRollbackFailureLog{
			Action: describeMergeRollback(failure.action),
			Error:  failure.err.Error(),
		}
	}
	m.log.Error(mergeFailureLog{
		Operation: "mod:mergeMods", GroupPath: request.GroupPath,
		Placement: request.Placement, PackName: request.PackName, Stage: "execute",
		Created: created, RollbackFailures: rollbackFailures, Error: err.Error(),
	}, "Mod:mergeMods:context")
}

func createUniqueMergeFolder(parent, name string, created *[]mergeRollback) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Merged"
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	existing := make([]string, 0, len(entries))
	for _, entry := range entries {
		existing = append(existing, entry.Name())
	}
	target := filepath.Join(parent, platformUniqueName(name, existing))
	if err := os.Mkdir(target, 0o755); err != nil {
		return "", err
	}
	*created = append(*created, mergeRollback{kind: "remove", path: target})
	return target, nil
}

func enabledMergeNames(packs []MergePackClassification, destination string) ([]string, error) {
	entries, err := os.ReadDir(destination)
	if err != nil {
		return nil, err
	}
	used := map[string]struct{}{}
	for _, entry := range entries {
		used[strings.ToLower(entry.Name())] = struct{}{}
	}
	result := make([]string, len(packs))
	for i, pack := range packs {
		base := stripDisabled(filepath.Base(pack.Path))
		if base == "" {
			base = filepath.Base(pack.Path)
		}
		name := base
		for counter := 2; ; counter++ {
			if _, exists := used[strings.ToLower(name)]; !exists {
				break
			}
			name = fmt.Sprintf("%s (%d)", base, counter)
		}
		used[strings.ToLower(name)] = struct{}{}
		result[i] = name
	}
	return result, nil
}

func disableOriginalForMerge(path string, created *[]mergeRollback) error {
	if isDisabled(filepath.Base(path)) {
		return nil
	}
	destination := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	for counter := 2; ; counter++ {
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			break
		}
		destination = filepath.Join(filepath.Dir(path), fmt.Sprintf("DISABLED %s (%d)", filepath.Base(path), counter))
	}
	if err := os.Rename(path, destination); err != nil {
		return err
	}
	*created = append(*created, mergeRollback{kind: "move", from: destination, to: path})
	return nil
}

func enablePackFolders(packs []MergePackClassification, created *[]mergeRollback) ([]MergePackClassification, error) {
	reservedByParent := map[string]map[string]struct{}{}
	destPaths := make([]string, len(packs))
	for i, pack := range packs {
		parent := filepath.Dir(pack.Path)
		used, ok := reservedByParent[parent]
		if !ok {
			entries, err := os.ReadDir(parent)
			if err != nil {
				return nil, err
			}
			used = map[string]struct{}{}
			for _, entry := range entries {
				used[strings.ToLower(entry.Name())] = struct{}{}
			}
			for _, other := range packs {
				if filepath.Dir(other.Path) != parent {
					continue
				}
				delete(used, strings.ToLower(filepath.Base(other.Path)))
			}
			reservedByParent[parent] = used
		}
		destPaths[i] = filepath.Join(parent, uniqueEnabledFolderName(pack.Path, used))
	}

	currentPaths := make([]string, len(packs))
	for i, pack := range packs {
		currentPaths[i] = pack.Path
	}
	for index := range packs {
		dest := destPaths[index]
		current := currentPaths[index]
		if sameMergePath(dest, current) {
			continue
		}
		conflictIndex := -1
		for otherIndex, itemPath := range currentPaths {
			if otherIndex != index && sameMergePath(itemPath, dest) {
				conflictIndex = otherIndex
				break
			}
		}
		if conflictIndex != -1 {
			tempPath, err := allocateStagePath(currentPaths[conflictIndex])
			if err != nil {
				return nil, err
			}
			if err := os.Rename(currentPaths[conflictIndex], tempPath); err != nil {
				return nil, err
			}
			*created = append(*created, mergeRollback{
				kind: "move", from: tempPath, to: currentPaths[conflictIndex],
			})
			currentPaths[conflictIndex] = tempPath
		}
		if err := os.Rename(current, dest); err != nil {
			return nil, err
		}
		*created = append(*created, mergeRollback{kind: "move", from: dest, to: current})
		currentPaths[index] = dest
	}

	result := make([]MergePackClassification, len(packs))
	for i, pack := range packs {
		if sameMergePath(currentPaths[i], pack.Path) {
			result[i] = pack
			continue
		}
		result[i] = remapPackPath(pack, currentPaths[i])
	}
	return result, nil
}

func uniqueEnabledFolderName(sourcePath string, used map[string]struct{}) string {
	currentName := filepath.Base(sourcePath)
	base := stripDisabled(currentName)
	if base == "" {
		base = currentName
	}
	name := base
	for counter := 1; ; counter++ {
		if _, exists := used[strings.ToLower(name)]; !exists {
			used[strings.ToLower(name)] = struct{}{}
			return name
		}
		name = fmt.Sprintf("%s (%d)", base, counter+1)
	}
}

func remapPackPath(pack MergePackClassification, nextPath string) MergePackClassification {
	next := pack
	next.Path = nextPath
	if pack.PrimaryIniPath != nil {
		relative, err := filepath.Rel(pack.Path, *pack.PrimaryIniPath)
		if err == nil {
			mapped := filepath.Join(nextPath, relative)
			next.PrimaryIniPath = &mapped
		}
	}
	return next
}

func allocateStagePath(sourcePath string) (string, error) {
	parent := filepath.Dir(sourcePath)
	baseName := filepath.Base(sourcePath)
	for counter := 1; counter <= 1000; counter++ {
		candidate := filepath.Join(parent, fmt.Sprintf("__nhd_stage_%d_%d_%s", time.Now().UnixMilli(), counter, baseName))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", errors.New("STAGE_PATH_CONFLICT")
}

func sameMergePath(left, right string) bool {
	return strings.EqualFold(resolveAgainst("", left), resolveAgainst("", right))
}

func disableINIForMerge(path string, created *[]mergeRollback) error {
	destination, err := allocateMergeINIBackup(path)
	if err != nil {
		return err
	}
	if err := os.Rename(path, destination); err != nil {
		return err
	}
	*created = append(*created, mergeRollback{kind: "move", from: destination, to: path})
	return nil
}

func ensureMergeBackup(path string, created *[]mergeRollback) error {
	directory := filepath.Dir(path)
	base := strings.ToLower(filepath.Base(path))
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name())
		if isExactMergeBackup(lower, base) {
			return nil
		}
	}
	destination, err := allocateMergeINIBackup(path)
	if err != nil {
		return err
	}
	input, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(destination, input, 0o644); err != nil {
		return err
	}
	*created = append(*created, mergeRollback{kind: "remove", path: destination})
	return nil
}

func isExactMergeBackup(name, base string) bool {
	suffix := "_" + strings.ToLower(base)
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, suffix) {
		return false
	}
	prefix := strings.TrimSuffix(lower, suffix)
	if prefix == "disabled_backup" {
		return true
	}
	number := strings.TrimPrefix(prefix, "disabled_backup_")
	if number == prefix || number == "" {
		return false
	}
	for _, char := range number {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func mergeOwnedPathError(err error) error {
	if err != nil && err.Error() == "MOD_PATH_OUTSIDE_MANAGED_ROOT" {
		return &mergeValidationError{message: outsideManagedModsMessage}
	}
	return err
}

func allocateMergeINIBackup(path string) (string, error) {
	directory, base := filepath.Dir(path), filepath.Base(path)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	used := map[string]struct{}{}
	for _, entry := range entries {
		used[strings.ToLower(entry.Name())] = struct{}{}
	}
	name, err := uniqueMergeDisabledName(base, used)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

func uniqueMergeDisabledName(fileName string, used map[string]struct{}) (string, error) {
	for counter := 1; counter <= 1000; counter++ {
		name := mergeDisabledBackupName(fileName, counter)
		if _, exists := used[strings.ToLower(name)]; exists {
			continue
		}
		used[strings.ToLower(name)] = struct{}{}
		return name, nil
	}
	return "", errors.New("MERGE_DISABLE_CONFLICT")
}

func mergeDisabledBackupName(fileName string, counter int) string {
	if strings.EqualFold(filepath.Ext(fileName), ".ini") {
		if counter == 1 {
			return "DISABLED_BACKUP_" + fileName
		}
		return fmt.Sprintf("DISABLED_BACKUP_%d_%s", counter, fileName)
	}
	base := stripDisabled(fileName)
	if base == "" {
		base = fileName
	}
	if counter == 1 {
		return "DISABLED " + base
	}
	return fmt.Sprintf("DISABLED %s (%d)", base, counter)
}

func recordMergeWrite(path string, created *[]mergeRollback) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		*created = append(*created, mergeRollback{kind: "remove", path: path})
		return nil
	}
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	*created = append(*created, mergeRollback{
		kind: "restore", path: path, content: content, mode: info.Mode().Perm(),
	})
	return nil
}

func classicEntryValue(section classicSection, key string) string {
	for _, entry := range section.entries {
		if !entry.control && strings.EqualFold(entry.key, key) {
			return entry.value
		}
	}
	return ""
}

func classicEntryValueDefault(section classicSection, key, fallback string) string {
	if value := classicEntryValue(section, key); value != "" {
		return value
	}
	return fallback
}

func classicHasEntry(section classicSection, key string) bool {
	return classicEntryValue(section, key) != ""
}

func isControlFlowLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "if\t") ||
		strings.HasPrefix(lower, "else if") || strings.HasPrefix(lower, "elif") ||
		lower == "else" || lower == "endif"
}

func isClassicResourceReference(key, value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "null" || lower == "auto" || strings.HasPrefix(lower, "commandlist") ||
		regexp.MustCompile(`^\d+(?:\.\d+)?$`).MatchString(trimmed) {
		return false
	}
	if regexp.MustCompile(`(?i)\bResource\w*`).MatchString(trimmed) {
		return true
	}
	return regexp.MustCompile(`(?i)^(?:vb|ib|ps-t|vs-t|ps-u|cs-u|cs-t)\d*`).MatchString(key) &&
		!regexp.MustCompile(`(?i)^[a-z_]+$`).MatchString(trimmed)
}

func appendClassicResourceSuffix(value string, index int) string {
	re := regexp.MustCompile(`(?i)\bResource[^\s,;]+`)
	location := re.FindStringIndex(value)
	if location == nil {
		return fmt.Sprintf("%s.%d", value, index)
	}
	target := value[location[0]:location[1]]
	if strings.HasSuffix(target, fmt.Sprintf(".%d", index)) {
		return value
	}
	return value[:location[0]] + fmt.Sprintf("%s.%d", target, index) + value[location[1]:]
}

func mergeRelativePath(base, target string) string {
	relative, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".") {
		return relative
	}
	return `.\` + relative
}

func mergeFolderName(node MergePlanNode, request MergeModsRequest) string {
	if value := strings.TrimSpace(node.Name); value != "" {
		return value
	}
	if value := strings.TrimSpace(request.PackName); value != "" {
		return value
	}
	return "Merged"
}

func platformUniqueName(name string, existing []string) string {
	used := map[string]struct{}{}
	for _, value := range existing {
		used[strings.ToLower(value)] = struct{}{}
	}
	result := name
	for counter := 2; ; counter++ {
		if _, exists := used[strings.ToLower(result)]; !exists {
			return result
		}
		result = fmt.Sprintf("%s (%d)", name, counter)
	}
}

func safeMergeName(name string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed != "" && trimmed != "." && trimmed != ".." &&
		!strings.ContainsRune(name, 0) && !strings.ContainsAny(name, "[]=\r\n;\"'$<>:/\\|?*")
}

func strictChildPath(parent, target string) bool {
	return pathWithin(parent, target) && !samePath(parent, target)
}
