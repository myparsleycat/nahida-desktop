package mod

import "strings"

const disabledFileSuffix = ".disabled"

func isDisabledFile(fileName string) bool {
	return strings.HasSuffix(strings.ToLower(fileName), disabledFileSuffix)
}

func stripDisabledFileSuffix(fileName string) string {
	if !isDisabledFile(fileName) {
		return fileName
	}
	return fileName[:len(fileName)-len(disabledFileSuffix)]
}

func isDisabledFolderName(folderName string) bool {
	return isDisabled(folderName)
}

func restoreDisabledPrefix(sourceFolderName, folderName string) string {
	match := disabledPrefixRE.FindString(strings.TrimSpace(sourceFolderName))
	if match == "" {
		return folderName
	}
	return match + folderName
}

func normalizeRelativePath(targetPath string) string {
	parts := strings.FieldsFunc(targetPath, func(r rune) bool { return r == '/' || r == '\\' })
	for i := range parts {
		parts[i] = strings.ToLower(stripDisabled(parts[i]))
	}
	return strings.Join(parts, "/")
}

func manualSubGroupSegmentMatches(entryName, storedSegment string) bool {
	return manualSegmentMatches(entryName, storedSegment)
}
