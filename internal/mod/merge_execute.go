package mod

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

func (m *Mod) MergeMods(ctx context.Context, request MergeModsRequest) (result MergeModsResult, err error) {
	if err := m.validateMergeRequest(ctx, request); err != nil {
		return result, err
	}
	leaves := []string{}
	collectMergeLeafPaths(request.Root, &leaves)
	blocked, release := m.guardActiveDownloadActions(leaves)
	if slices.Contains(blocked, true) {
		release()
		return result, errors.New("MOD_DOWNLOAD_IN_PROGRESS")
	}
	defer release()
	created := []mergeRollback{}
	defer func() {
		if err != nil {
			failures := rollbackMerge(created)
			err = m.logMergeFailure(request, created, failures, err)
		}
	}()
	output, err := m.executeMergeNode(ctx, request.Root, request, &created)
	if err != nil {
		return result, err
	}
	return MergeModsResult{OutputPath: output}, nil
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
