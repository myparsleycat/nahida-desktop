package tools

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	modelViewerNumericSuffixRE = regexp.MustCompile(`\.([0-9]+)$`)
	modelViewerMeshFamilyRE    = regexp.MustCompile(`(?i)(head|body|dress|hair|face|weapon|cloth|skirt|shoe|arm|leg|hand|foot)(?:[a-z0-9]+)?$`)
)

func modelViewerTrimResourcePrefix(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "ref ") {
		value = strings.TrimSpace(value[4:])
	}
	if strings.HasPrefix(strings.ToLower(value), "resource") {
		value = value[len("resource"):]
	}
	return value
}

func modelViewerNumericSuffix(value string) (int, bool) {
	match := modelViewerNumericSuffixRE.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	parsed, err := strconv.Atoi(match[1])
	return parsed, err == nil
}

func modelViewerCanonicalMeshKey(value string) string {
	suffix, hasSuffix := modelViewerNumericSuffix(value)
	value = modelViewerNumericSuffixRE.ReplaceAllString(value, "")
	value = modelViewerTrimResourcePrefix(value)
	lower := strings.ToLower(value)
	if strings.HasSuffix(lower, "indexbuffer") {
		value = value[:len(value)-len("indexbuffer")]
	} else if strings.HasSuffix(lower, "ib") {
		value = value[:len(value)-2]
	}
	value = modelViewerMeshFamilyRE.ReplaceAllString(value, "$1")
	if hasSuffix {
		value += "." + strconv.Itoa(suffix)
	}
	return value
}

func modelViewerKeyMatches(groupKey, ibKey string, strict bool) bool {
	a, b := modelViewerNormalizeKey(modelViewerCanonicalMeshKey(groupKey)), modelViewerNormalizeKey(modelViewerCanonicalMeshKey(ibKey))
	if a == b {
		return true
	}
	groupSuffix, groupHas := modelViewerNumericSuffix(groupKey)
	ibSuffix, ibHas := modelViewerNumericSuffix(ibKey)
	if groupHas || ibHas {
		if strict && (!groupHas || !ibHas || groupSuffix != ibSuffix) || groupHas && ibHas && groupSuffix != ibSuffix {
			return false
		}
		groupBase := modelViewerNormalizeKey(modelViewerNumericSuffixRE.ReplaceAllString(modelViewerCanonicalMeshKey(groupKey), ""))
		ibBase := modelViewerNormalizeKey(modelViewerNumericSuffixRE.ReplaceAllString(modelViewerCanonicalMeshKey(ibKey), ""))
		if strict {
			return groupBase == ibBase
		}
		return groupBase == ibBase || strings.Contains(groupBase, ibBase) || strings.Contains(ibBase, groupBase)
	}
	if strict {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

func modelViewerBestKeyForIB(stem, resourceName string, keys []string) string {
	normalizedStem := modelViewerNormalizeKey(modelViewerCanonicalMeshKey(stem))
	normalizedName := modelViewerNormalizeKey(modelViewerCanonicalMeshKey(resourceName))
	sorted := append([]string(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	suffix, hasSuffix := modelViewerNumericSuffix(resourceName)
	if !hasSuffix {
		suffix, hasSuffix = modelViewerNumericSuffix(stem)
	}
	var sameSuffix []string
	if hasSuffix {
		for _, key := range sorted {
			if value, ok := modelViewerNumericSuffix(key); ok && value == suffix {
				sameSuffix = append(sameSuffix, key)
			}
		}
	}
	for _, pool := range [][]string{sameSuffix, sorted} {
		for _, key := range pool {
			normalized := modelViewerNormalizeKey(modelViewerCanonicalMeshKey(key))
			if normalized == normalizedName || normalized == normalizedStem {
				return key
			}
		}
	}
	if len(sameSuffix) == 1 {
		return sameSuffix[0]
	}
	baseMatches := func(key string) bool {
		key = strings.TrimRight(modelViewerNormalizeKey(modelViewerCanonicalMeshKey(key)), "0123456789")
		stemBase := strings.TrimRight(normalizedStem, "0123456789")
		nameBase := strings.TrimRight(normalizedName, "0123456789")
		return strings.Contains(stemBase, key) || strings.Contains(nameBase, key) || strings.Contains(key, stemBase) || strings.Contains(key, nameBase)
	}
	for _, pool := range [][]string{sameSuffix, sorted} {
		for _, key := range pool {
			if baseMatches(key) {
				return key
			}
		}
	}
	return stem
}
