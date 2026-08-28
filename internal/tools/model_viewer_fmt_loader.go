package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type modelViewerIbResource struct {
	Name           string
	Filename       string
	Format         string
	Key            string
	OverrideHash   string
	OverrideHashes []string
}

func loadModelViewerFmt(modDir, assetDir string, ib modelViewerIbResource, stride int, layout string) (modelViewerFmtLayout, error) {
	stem := strings.TrimSuffix(filepath.Base(ib.Filename), filepath.Ext(ib.Filename))
	local := filepath.Join(modDir, stem+".fmt")
	if text, err := os.ReadFile(local); err == nil {
		return parseModelViewerFmt(string(text), stride, ib.Format)
	}
	assetFiles, err := walkModelViewerFiles(assetDir)
	if err != nil {
		return modelViewerFmtLayout{}, err
	}
	for _, file := range assetFiles {
		if strings.EqualFold(filepath.Ext(file), ".fmt") && strings.Contains(modelViewerNormalizeKey(filepath.Base(file)), modelViewerNormalizeKey(stem)) {
			text, readErr := os.ReadFile(file)
			if readErr != nil {
				return modelViewerFmtLayout{}, readErr
			}
			return parseModelViewerFmt(string(text), stride, ib.Format)
		}
	}
	if layout == "wwmi" {
		if found, ok, findErr := findModelViewerWwmiFmt(assetFiles, ib, stride); findErr != nil || ok {
			return found, findErr
		}
	}
	vb0Path := ""
	for _, file := range assetFiles {
		base := strings.ToLower(filepath.Base(file))
		if strings.EqualFold(filepath.Ext(file), ".txt") && strings.Contains(base, "vb0") && strings.Contains(modelViewerNormalizeKey(base), modelViewerNormalizeKey(stem)) {
			vb0Path = file
			break
		}
	}
	if vb0Path == "" {
		for _, hash := range modelViewerHashCandidates(ib) {
			ibBase := ""
			for _, file := range assetFiles {
				base := strings.ToLower(filepath.Base(file))
				if strings.EqualFold(filepath.Ext(file), ".txt") && strings.Contains(base, "-ib=") && strings.Contains(modelViewerNormalizeKey(base), hash) {
					ibBase = strings.SplitN(filepath.Base(file), "-ib=", 2)[0]
					break
				}
			}
			if ibBase == "" {
				continue
			}
			for _, file := range assetFiles {
				base := strings.ToLower(filepath.Base(file))
				if strings.EqualFold(filepath.Ext(file), ".txt") && strings.Contains(base, "vb0") && strings.HasPrefix(base, strings.ToLower(ibBase)) {
					vb0Path = file
					break
				}
			}
			if vb0Path != "" {
				break
			}
		}
	}
	if vb0Path == "" {
		return modelViewerFmtLayout{}, fmt.Errorf("no matching .fmt or *-vb0.txt found for %s under %s", ib.Filename, assetDir)
	}
	text, err := os.ReadFile(vb0Path)
	if err != nil {
		return modelViewerFmtLayout{}, err
	}
	return parseModelViewerFmt(extractModelViewerFmtFromVB0(string(text), stride, ib.Format), stride, ib.Format)
}

func findModelViewerWwmiFmt(files []string, ib modelViewerIbResource, stride int) (modelViewerFmtLayout, bool, error) {
	for _, hash := range modelViewerHashCandidates(ib) {
		var candidates []string
		for _, file := range files {
			if strings.EqualFold(filepath.Ext(file), ".fmt") && strings.Contains(modelViewerNormalizeKey(file), hash) {
				candidates = append(candidates, file)
			}
		}
		if layout, ok, err := chooseModelViewerFmt(candidates, stride, ib.Format); err != nil || ok {
			return layout, ok, err
		}
	}
	for _, file := range files {
		if !strings.EqualFold(filepath.Base(file), "Metadata.json") {
			continue
		}
		var metadata struct {
			VB0Hash string `json:"vb0_hash"`
		}
		raw, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(raw, &metadata) != nil {
			continue
		}
		match := false
		for _, hash := range modelViewerHashCandidates(ib) {
			if modelViewerNormalizeKey(metadata.VB0Hash) == hash {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		var candidates []string
		for _, candidate := range files {
			if filepath.Dir(candidate) == filepath.Dir(file) && strings.HasPrefix(strings.ToLower(filepath.Base(candidate)), "component ") && strings.EqualFold(filepath.Ext(candidate), ".fmt") {
				candidates = append(candidates, candidate)
			}
		}
		if layout, ok, chooseErr := chooseModelViewerFmt(candidates, stride, ib.Format); chooseErr != nil || ok {
			return layout, ok, chooseErr
		}
	}
	return modelViewerFmtLayout{}, false, nil
}

func chooseModelViewerFmt(paths []string, stride int, indexFormat string) (modelViewerFmtLayout, bool, error) {
	type candidate struct {
		path   string
		layout modelViewerFmtLayout
	}
	parsed := make([]candidate, 0, len(paths))
	for _, path := range paths {
		text, err := os.ReadFile(path)
		if err != nil {
			return modelViewerFmtLayout{}, false, err
		}
		layout, err := parseModelViewerFmt(string(text), stride, indexFormat)
		if err != nil {
			return modelViewerFmtLayout{}, false, err
		}
		parsed = append(parsed, candidate{path: path, layout: layout})
	}
	if len(parsed) == 0 {
		return modelViewerFmtLayout{}, false, nil
	}
	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].layout.Stride != parsed[j].layout.Stride {
			return parsed[i].layout.Stride < parsed[j].layout.Stride
		}
		return parsed[i].path < parsed[j].path
	})
	return parsed[0].layout, true, nil
}

func modelViewerHashCandidates(ib modelViewerIbResource) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	values := append(append([]string(nil), ib.OverrideHashes...), ib.OverrideHash, ib.Key)
	for _, value := range values {
		normalized := modelViewerNormalizeKey(value)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	return out
}

func walkModelViewerFiles(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
