package mod

import (
	"regexp"
	"testing"
)

func TestWrapNamespaceHashesKeepsNestedDrawTypeBranches(t *testing.T) {
	input := `[TextureOverrideCharCHairBlend]
hash = beef0002
handling = skip
vb2 = ResourceCharCHairBlend
if DRAW_TYPE == 1
	vb0 = ResourceCharCHairPosition
	draw = 2981, 0
endif
if DRAW_TYPE == 8
	Resource\SRMI\PositionBuffer = ref ResourceCharCHairPositionCS
	$\SRMI\vertcount = 2981
endif
`
	wrapped := wrapNamespaceHashes(input, "CharCMerge", 0)
	want := regexp.MustCompile(`(?s)hash = beef0002\nmatch_priority = 0\nif \$\\CharCMerge\\Master\\swapvar==0\n\thandling = skip\n\tvb2 = ResourceCharCHairBlend\n\tif DRAW_TYPE == 1\n\t\tvb0 = ResourceCharCHairPosition\n\t\tdraw = 2981, 0\n\tendif\n\tif DRAW_TYPE == 8\n\t\tResource\\SRMI\\PositionBuffer = ref ResourceCharCHairPositionCS\n\t\t\$\\SRMI\\vertcount = 2981\n\tendif\nendif`)
	if !want.MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
	if regexp.MustCompile(`else if \$\\CharCMerge\\Master\\swapvar`).MatchString(wrapped) {
		t.Fatalf("unexpected extra swapvar branch: %s", wrapped)
	}
}

func TestWrapNamespaceHashesFlushesBeforeIndentedSectionHeaders(t *testing.T) {
	input := `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
  [ResourcePosition]
type = Buffer
`
	wrapped := wrapNamespaceHashes(input, "CharA", 0)
	if !regexp.MustCompile(`endif\n\n  \[ResourcePosition\]`).MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
}

func TestWrapNamespaceHashesKeepsMultipleVB0LinesInOneBranch(t *testing.T) {
	input := `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition0
vb0 = ResourcePosition1
ps-t0 = ResourceTexture
`
	wrapped := wrapNamespaceHashes(input, "CharA", 0)
	want := regexp.MustCompile(`hash = abcdef01\nmatch_priority = 0\nif \$\\CharA\\Master\\swapvar==0\n\tvb0 = ResourcePosition0\n\tvb0 = ResourcePosition1\n\tps-t0 = ResourceTexture\nendif`)
	if !want.MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
	if regexp.MustCompile(`else if \$\\CharA\\Master\\swapvar==1`).MatchString(wrapped) {
		t.Fatalf("unexpected extra swapvar branch: %s", wrapped)
	}
}

func TestWrapNamespaceHashesKeepsEFMIMatchIndexCountOutsideWrap(t *testing.T) {
	input := `[TextureOverride_Component0]
hash = beef0003
match_priority = 0
match_index_count = 48909
$object_detected = 1
$lod_level = 0
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
`
	wrapped := wrapNamespaceHashes(input, "CharD", 1)
	want := regexp.MustCompile(`(?s)hash = beef0003\nmatch_priority = 1\nmatch_index_count = 48909\nif \$\\CharD\\Master\\swapvar==1\n\t\$object_detected = 1\n\t\$lod_level = 0\n\tif \$mod_enabled && DRAW_TYPE == 4\n\t    handling = skip\n\t    run = CommandList_Draw_Component0\n\tendif\nendif`)
	if !want.MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
	if regexp.MustCompile(`if \$\\CharD\\Master\\swapvar==1\n\tmatch_index_count`).MatchString(wrapped) {
		t.Fatalf("match_index_count was wrapped: %s", wrapped)
	}
	if regexp.MustCompile(`match_priority = 0`).MatchString(wrapped) {
		t.Fatalf("old match_priority kept: %s", wrapped)
	}
}

func TestWrapNamespaceHashesHoistsMatchFilterAfterRuntimeCommands(t *testing.T) {
	input := `[TextureOverride_Component0]
hash = beef0003
$object_detected = 1
match_index_count = 48909
handling = skip
`
	wrapped := wrapNamespaceHashes(input, "CharD", 1)
	want := regexp.MustCompile(`hash = beef0003\nmatch_priority = 1\nmatch_index_count = 48909\nif \$\\CharD\\Master\\swapvar==1\n\t\$object_detected = 1\n\thandling = skip\nendif`)
	if !want.MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
}

func TestWrapNamespaceHashesRewritesExistingMatchPriority(t *testing.T) {
	input := `[TextureOverride_Texture0]
hash = beef0004
match_priority = 0
if $object_detected
    this = Resource_Texture0
endif
`
	wrapped := wrapNamespaceHashes(input, "CharD", 1)
	want := regexp.MustCompile(`(?s)hash = beef0004\nmatch_priority = 1\nif \$\\CharD\\Master\\swapvar==1\n\tif \$object_detected\n\t    this = Resource_Texture0\n\tendif\nendif`)
	if !want.MatchString(wrapped) {
		t.Fatalf("wrapped = %s", wrapped)
	}
	if regexp.MustCompile(`match_priority = 0`).MatchString(wrapped) {
		t.Fatalf("old match_priority kept: %s", wrapped)
	}
}
