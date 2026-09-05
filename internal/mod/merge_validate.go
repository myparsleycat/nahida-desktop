package mod

import (
	"context"
	"path/filepath"
	"strings"
)

const (
	maxMergePlanDepth = 32
	maxMergePlanNodes = 256

	invalidMergeRequestMessage = "Invalid merge request payload"
	outsideManagedModsMessage  = "Path is outside the managed mod folder"
	outsideMergeGroupMessage   = "Path is outside the selected group"
)

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
		release, err := m.rejectActiveDownloadAction(leaf)
		if err != nil {
			return err
		}
		release()
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

func collectMergeLeafPaths(node MergePlanNode, leaves *[]string) {
	if node.Kind == "leaf" {
		*leaves = append(*leaves, node.Path)
		return
	}
	for _, child := range node.Children {
		collectMergeLeafPaths(child, leaves)
	}
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

func mergeOwnedPathError(err error) error {
	if err != nil && err.Error() == "MOD_PATH_OUTSIDE_MANAGED_ROOT" {
		return &mergeValidationError{message: outsideManagedModsMessage}
	}
	return err
}

func safeMergeName(name string) bool {
	trimmed := strings.TrimSpace(name)
	return trimmed != "" && trimmed != "." && trimmed != ".." &&
		!strings.ContainsRune(name, 0) && !strings.ContainsAny(name, "[]=\r\n;\"'$<>:/\\|?*")
}

func strictChildPath(parent, target string) bool {
	return pathWithin(parent, target) && !samePath(parent, target)
}
