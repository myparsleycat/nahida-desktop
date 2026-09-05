package mod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
