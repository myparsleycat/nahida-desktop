package mod

import (
	"strings"
)

var threedmigotoImporters = []string{"GIMI", "SRMI", "ZZMI", "HIMI", "WWMI", "EFMI"}

// IsThreedmigotoImporter reports whether importer loads 3DMigoto INI mods, which MergeMods can combine.
func IsThreedmigotoImporter(importer string) bool {
	for _, key := range threedmigotoImporters {
		if strings.EqualFold(importer, key) {
			return true
		}
	}
	return false
}

// DefaultMergePlan groups every mergeable pack under one root, using classic merge when every pack allows it.
// It returns nil when only support packs were selected.
func DefaultMergePlan(result ClassifyMergePacksResult, packName string) *MergePlanNode {
	var children []MergePlanNode
	engine := "classic"
	for _, pack := range result.Packs {
		if pack.Family == "support" {
			continue
		}
		if !pack.AllowsClassic {
			engine = "namespace"
		}
		children = append(children, MergePlanNode{Kind: "leaf", Path: pack.Path})
	}
	if len(children) == 0 {
		return nil
	}
	name := strings.Join(strings.Fields(packName), "")
	if !SafeMergeName(name) {
		name = "Merged"
	}
	return &MergePlanNode{
		Kind:           "group",
		ID:             "root",
		Engine:         engine,
		Name:           name,
		ForwardKey:     "vk_right",
		BackKey:        "vk_left",
		IncludeVanilla: engine == "namespace",
		Children:       children,
	}
}

func mergePlanLeaves(node MergePlanNode) []string {
	if node.Kind == "leaf" {
		return []string{node.Path}
	}
	var leaves []string
	for _, child := range node.Children {
		leaves = append(leaves, mergePlanLeaves(child)...)
	}
	return leaves
}

// MergePlanHasClassicViolation reports whether a classic group of node holds a pack that only namespace merge
// supports, or one that was not classified.
func MergePlanHasClassicViolation(node MergePlanNode, packs map[string]MergePackClassification) bool {
	if node.Kind == "leaf" {
		return false
	}
	if node.Engine == "classic" {
		for _, path := range mergePlanLeaves(node) {
			if pack, ok := packs[path]; !ok || !pack.AllowsClassic {
				return true
			}
		}
	}
	for _, child := range node.Children {
		if MergePlanHasClassicViolation(child, packs) {
			return true
		}
	}
	return false
}

// MergePlanValid checks the parts of node the user edits: names, keys and at least two packs per group.
func MergePlanValid(node MergePlanNode) bool {
	if node.Kind == "leaf" {
		return true
	}
	if !SafeMergeName(node.Name) || strings.TrimSpace(node.ForwardKey) == "" ||
		node.Engine == "namespace" && strings.TrimSpace(node.BackKey) == "" || len(mergePlanLeaves(node)) < 2 {
		return false
	}
	for _, child := range node.Children {
		if !MergePlanValid(child) {
			return false
		}
	}
	return true
}
