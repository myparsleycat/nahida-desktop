package mod

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

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
