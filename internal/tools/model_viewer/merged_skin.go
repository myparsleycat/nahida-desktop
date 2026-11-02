package modelviewer

import (
	"strconv"
	"strings"
)

// collectModelViewerMergedSkinStarts finds carriers of a merged skeleton, whose
// compute shader skins each Position buffer into one merged buffer the draws then
// index. It maps the Position resource to the merged row its vertices start at.
func collectModelViewerMergedSkinStarts(root, shaderBaseDir string, sections []modINISection) map[string]int {
	starts := make(map[string]int)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") {
			continue
		}
		shaderPath := stripModelViewerQuotes(sectionValue(section.Lines, "cs"))
		carrier := modelViewerResourceToken(sectionValue(section.Lines, "cs-t0"))
		start, err := strconv.Atoi(sectionValue(section.Lines, "y1"))
		if shaderPath == "" || carrier == "" || err != nil || start <= 0 {
			continue
		}
		shader, ok := readModelViewerComputeShader(root, shaderBaseDir, shaderPath)
		if !ok || !isModelViewerMergedSkinShader(shader) {
			continue
		}
		starts[modelViewerNormalizeKey(carrier)] = start
	}
	return starts
}

func isModelViewerMergedSkinShader(shader string) bool {
	compact := compactModelViewerShader(shader)
	for _, signature := range []string{
		"structuredbuffer<zzvertex40>src_rows:register(t0)",
		"rwstructuredbuffer<float>dst_rows:register(u0)",
		"uintdest_start=(uint)param.y;",
		"zz_write_row(dest_start+index,",
	} {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	return true
}
