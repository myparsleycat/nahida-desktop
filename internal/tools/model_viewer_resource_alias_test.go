package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModelViewerEffectiveResourcesFollowsUAVChain(t *testing.T) {
	sections := parseModINI(`[Constants]
post ResourceBodyPosition = copy_desc ResourcBodyPosition.2
[CustomShaderShape]
cs-u5 = copy ResourceBodyPosition.2
ResourceBodyPosition.1 = ref cs-u5
[CustomShaderPose]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.1]
[ResourceBodyPosition.2]
filename = body.buf
stride = 40`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	byName := make(map[string]modelViewerResource)
	for _, resource := range resources {
		byName[resource.Name] = resource
	}
	for _, name := range []string{"BodyPosition", "BodyPosition.1", "BodyPosition.2"} {
		if got := byName[name]; got.Filename != "body.buf" || got.Stride != 40 {
			t.Fatalf("%s = %#v", name, got)
		}
	}
}

func TestResolveModelViewerEffectiveResourcesAtRejectsMissingAndOutsideLeaves(t *testing.T) {
	root := t.TempDir()
	outsideName := filepath.Base(root) + "-outside.buf"
	outside := filepath.Join(filepath.Dir(root), outsideName)
	if err := os.WriteFile(outside, make([]byte, 40), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	for _, filename := range []string{"missing.buf", "../" + outsideName} {
		sections := parseModINI(fmt.Sprintf(`[CustomShader]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.1]
filename = %s
stride = 40`, filename))
		resources := collectModelViewerResources(sections)
		resolved := resolveModelViewerEffectiveResourcesAt(root, root, sections, resources)
		if resolved[0].Filename != "" {
			t.Fatalf("%s resolved outside the safe mod root: %#v", filename, resolved[0])
		}
	}
}

func TestResolveModelViewerEffectiveResourcesPrefersCopyDescAndRejectsAmbiguity(t *testing.T) {
	sections := parseModINI(`[Constants]
post ResourceBodyPosition = copy_desc ResourceBodyPosition.0
[CustomShaderA]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[CustomShaderB]
cs-u5 = copy ResourceBodyPosition.2
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.0]
filename = preferred.buf
stride = 40
[ResourceBodyPosition.1]
filename = one.buf
stride = 40
[ResourceBodyPosition.2]
filename = two.buf
stride = 40`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if got := resources[0]; got.Filename != "preferred.buf" {
		t.Fatalf("effective position = %#v", got)
	}

	sections = parseModINI(`[CustomShaderA]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[CustomShaderB]
cs-u5 = copy ResourceBodyPosition.2
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.1]
filename = one.buf
stride = 40
[ResourceBodyPosition.2]
filename = two.buf
stride = 40`)
	resources = resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if got := resources[0]; got.Filename != "" {
		t.Fatalf("ambiguous position = %#v", got)
	}

	sections = parseModINI(`[Constants]
post ResourceBodyPosition = copy_desc ResourceBodyPosition.0
post ResourceBodyPosition = copy_desc ResourceBodyPosition.1
[CustomShader]
cs-u5 = copy ResourceBodyPosition.0
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.0]
filename = one.buf
stride = 40
[ResourceBodyPosition.1]
filename = two.buf
stride = 40`)
	resources = resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if got := resources[0]; got.Filename != "" {
		t.Fatalf("ambiguous copy_desc fell through to UAV source: %#v", got)
	}
}

func TestResolveModelViewerEffectiveResourcesPrefersDefaultStateUAV(t *testing.T) {
	sections := parseModINI(`[Constants]
global $mode = 0
[CustomShaderA]
if $mode == 0
  cs-u5 = copy ResourceBodyPosition.1
  ResourceBodyPosition = ref cs-u5
else
  cs-u5 = copy ResourceBodyPosition.2
  ResourceBodyPosition = ref cs-u5
endif
[ResourceBodyPosition]
[ResourceBodyPosition.1]
filename = default.buf
stride = 40
[ResourceBodyPosition.2]
filename = alternate.buf
stride = 40`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if got := resources[0]; got.Filename != "default.buf" {
		t.Fatalf("default-state UAV source = %#v", got)
	}
}

func TestResolveModelViewerEffectiveResourcesRejectsCycles(t *testing.T) {
	sections := parseModINI(`[CustomShaderA]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[CustomShaderB]
cs-u5 = copy ResourceBodyPosition
ResourceBodyPosition.1 = ref cs-u5
[ResourceBodyPosition]
[ResourceBodyPosition.1]`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if resources[0].Filename != "" || resources[1].Filename != "" {
		t.Fatalf("cycle resolved unexpectedly: %#v", resources)
	}
}

func TestResolveModelViewerEffectiveResourcesKeepsDirectFilename(t *testing.T) {
	sections := parseModINI(`[CustomShaderA]
cs-u5 = copy ResourceBodyPosition.1
ResourceBodyPosition = ref cs-u5
[ResourceBodyPosition]
filename = direct.buf
stride = 40
[ResourceBodyPosition.1]
filename = indirect.buf
stride = 40`)
	resources := resolveModelViewerEffectiveResources(sections, collectModelViewerResources(sections))
	if got := resources[0]; got.Filename != "direct.buf" {
		t.Fatalf("direct filename was replaced: %#v", got)
	}
}

func TestResolveModelViewerEffectiveResourcesRejectsMoreThan32Hops(t *testing.T) {
	var ini strings.Builder
	for index := range maxModelViewerResourceAliasDepth + 1 {
		fmt.Fprintf(&ini, "[CustomShader%d]\ncs-u5 = copy ResourceBodyPosition.%d\nResourceBodyPosition.%d = ref cs-u5\n", index, index+1, index)
	}
	fmt.Fprintf(&ini, "[ResourceBodyPosition]\n")
	for index := 1; index <= maxModelViewerResourceAliasDepth+1; index++ {
		fmt.Fprintf(&ini, "[ResourceBodyPosition.%d]\n", index)
	}
	ini.WriteString("filename = leaf.buf\nstride = 40\n")
	resources := resolveModelViewerEffectiveResources(parseModINI(ini.String()), collectModelViewerResources(parseModINI(ini.String())))
	if resources[0].Filename != "" {
		t.Fatalf("overlong chain resolved unexpectedly: %#v", resources[0])
	}
}
