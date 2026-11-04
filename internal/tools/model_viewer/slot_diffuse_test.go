package modelviewer

import (
	"encoding/binary"
	"fmt"
	"image/color"
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestLoadModViewerGIMINormalFirstLightMaps(t *testing.T) {
	for _, withLightMap := range []bool{true, false} {
		t.Run(fmt.Sprintf("light map=%t", withLightMap), func(t *testing.T) {
			dir := t.TempDir()
			writeTextureFile(t, dir, "BodyDiffuse.dds", efmiSolidDDS(16, color.NRGBA{R: 128, G: 128, B: 255, A: 255}))
			for _, suffix := range []string{"A", "B"} {
				writeTextureFile(t, dir, "BodyLightMap"+suffix+".dds", encodeWwmiColorDDS(16))
				writeTextureFile(t, dir, "BodyShadow"+suffix+".dds", efmiSolidDDS(16, color.NRGBA{G: 128, A: 255}))
			}
			lightA, lightB := "", ""
			if withLightMap {
				lightA = "ps-t2 = ResourceBodyShadowA"
				lightB = "ps-t2 = ResourceBodyShadowB"
			}
			fixture := loadViewerFixture(t, dir, fmt.Sprintf(`[Constants]
global persist $swap = 0
[KeySwap]
key = k
type = cycle
$swap = 0, 1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
ps-t0 = ResourceBodyDiffuse
if $swap == 0
ps-t1 = ResourceBodyLightMapA
%s
else
ps-t1 = ResourceBodyLightMapB
%s
endif
drawindexed = 3, 0, 0
[ResourceBodyDiffuse]
filename = BodyDiffuse.dds
[ResourceBodyLightMapA]
filename = BodyLightMapA.dds
[ResourceBodyLightMapB]
filename = BodyLightMapB.dds
[ResourceBodyShadowA]
filename = BodyShadowA.dds
[ResourceBodyShadowB]
filename = BodyShadowB.dds
`, lightA, lightB)+viewerBodyResources)

			for swap, suffix := range []string{"A", "B"} {
				state := evaluateViewerTransport(fixture.result, map[string]any{"swap": swap})
				if len(state.Meshes) != 1 || !state.Meshes[0].Visible {
					t.Fatalf("swap %d meshes = %+v", swap, state.Meshes)
				}
				mesh := state.Meshes[0]
				if !strings.HasSuffix(mesh.TexKey, "BodyLightMap"+suffix+".dds") {
					t.Fatalf("swap %d diffuse = %q", swap, mesh.TexKey)
				}
				if !withLightMap {
					if mesh.LightMapKey != "" {
						t.Fatalf("color texture reused as light map: %q", mesh.LightMapKey)
					}
					continue
				}
				if !strings.HasSuffix(mesh.LightMapKey, "BodyShadow"+suffix+".dds") ||
					fixture.result.Textures[mesh.LightMapKey].Role != "light_map" {
					t.Fatalf("swap %d light map = %q", swap, mesh.LightMapKey)
				}
			}
		})
	}
}

func encodeDX10HeaderDDS(width, height, dxgi uint32) []byte {
	header := make([]byte, 148)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[12:16], height)
	binary.LittleEndian.PutUint32(header[16:20], width)
	copy(header[84:88], "DX10")
	binary.LittleEndian.PutUint32(header[128:132], dxgi)
	return header
}

func TestAttachSlotBoundDiffuseReplacesGIMINormalMapNamedDiffuse(t *testing.T) {
	const side = 16
	dir := t.TempDir()
	olive := color.NRGBA{R: 127, G: 127, B: 2, A: 255}
	writeTextureFile(
		t,
		dir,
		"Normal.dds",
		efmiDX10DDS(side, efmiDXGIBGRA8, slices.Repeat([]color.NRGBA{olive}, side*side)),
	)
	writeTextureFile(t, dir, "Color.dds", encodeWwmiColorDDS(side))
	writeTextureFile(t, dir, "Skin.dds", encodeWwmiColorDDS(side))
	writeTextureFile(t, dir, "Linear.dds", efmiSolidDDS(side, efmiTestLightMap))
	rgbNormal := slices.Repeat([]color.NRGBA{{R: 128, G: 128, B: 255, A: 255}}, side*side)
	writeTextureFile(t, dir, "RGBNormal.dds", efmiDX10DDS(side, efmiDXGIBGRA8, rgbNormal))
	rgbNormal[0] = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	rgbNormal[1] = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	writeTextureFile(t, dir, "RGBNormalOutliers.dds", efmiDX10DDS(side, efmiDXGIBGRA8, rgbNormal))
	writeTextureFile(t, dir, "SRGBNormal.dds", efmiDX10DDS(side, efmiDXGIBGRA8SRGB, rgbNormal))
	writeTextureFile(t, dir, "BlueColor.dds", efmiDX10DDS(side, efmiDXGIBGRA8,
		slices.Repeat([]color.NRGBA{{R: 128, G: 128, B: 200, A: 255}}, side*side)))
	legacy := make([]color.NRGBA, 0, side*side)
	for y := range side {
		for x := range side {
			legacy = append(legacy, color.NRGBA{R: uint8(x * 8), G: uint8(y * 3), B: uint8((x + y) * 4), A: 255})
		}
	}
	writeTextureFile(t, dir, "Legacy.dds", encodeWwmiUncompressedDDS(side, side, legacy))

	skin := ModelViewerDNF{{{Var: "skin", Value: "1"}}}
	assign := func(role, file string, conditions ModelViewerDNF) modelViewerDirectTextureAssignment {
		return modelViewerDirectTextureAssignment{role: role, resource: file, file: file, conditions: conditions}
	}
	for _, tt := range []struct {
		name          string
		diffuse       string
		lightMaps     []modelViewerDirectTextureAssignment
		slots         []modelViewerDirectTextureAssignment
		wantDiffuses  []string
		wantLightMaps []string
	}{
		{
			name:    "RGB normal map keeps texture toggles",
			diffuse: "RGBNormal.dds",
			lightMaps: []modelViewerDirectTextureAssignment{
				assign("light_map", "Color.dds", modelViewerDNFTrue()),
				assign("light_map", "Skin.dds", skin),
			},
			wantDiffuses: []string{"Color.dds", "Skin.dds"},
		},
		{
			name:         "RGB normal map with sparse channel extremes",
			diffuse:      "RGBNormalOutliers.dds",
			lightMaps:    []modelViewerDirectTextureAssignment{assign("light_map", "Color.dds", modelViewerDNFTrue())},
			wantDiffuses: []string{"Color.dds"},
		},
		{
			name:          "sRGB diffuse resembling a normal map stays",
			diffuse:       "SRGBNormal.dds",
			lightMaps:     []modelViewerDirectTextureAssignment{assign("light_map", "Color.dds", modelViewerDNFTrue())},
			wantDiffuses:  []string{"SRGBNormal.dds"},
			wantLightMaps: []string{"Color.dds"},
		},
		{
			name:          "blue color diffuse stays",
			diffuse:       "BlueColor.dds",
			lightMaps:     []modelViewerDirectTextureAssignment{assign("light_map", "Color.dds", modelViewerDNFTrue())},
			wantDiffuses:  []string{"BlueColor.dds"},
			wantLightMaps: []string{"Color.dds"},
		},
		{
			name:    "light map names keep toggles",
			diffuse: "Normal.dds",
			lightMaps: []modelViewerDirectTextureAssignment{
				assign("light_map", "Color.dds", modelViewerDNFTrue()),
				assign("light_map", "Skin.dds", skin),
			},
			wantDiffuses: []string{"Color.dds", "Skin.dds"},
		},
		{
			name:    "undeclared variant follows the sRGB map",
			diffuse: "Normal.dds",
			lightMaps: []modelViewerDirectTextureAssignment{
				assign("light_map", "Color.dds", modelViewerDNFTrue()),
				assign("light_map", "Legacy.dds", skin),
			},
			wantDiffuses: []string{"Color.dds", "Legacy.dds"},
		},
		{
			name:    "linear variant stays a light map",
			diffuse: "Normal.dds",
			lightMaps: []modelViewerDirectTextureAssignment{
				assign("light_map", "Color.dds", modelViewerDNFTrue()),
				assign("light_map", "Linear.dds", skin),
			},
			wantDiffuses:  []string{"Color.dds"},
			wantLightMaps: []string{"Linear.dds"},
		},
		{
			name:          "undeclared map alone proves nothing",
			diffuse:       "Normal.dds",
			lightMaps:     []modelViewerDirectTextureAssignment{assign("light_map", "Legacy.dds", modelViewerDNFTrue())},
			wantDiffuses:  []string{"Normal.dds"},
			wantLightMaps: []string{"Legacy.dds"},
		},
		{
			name:         "unnamed ps-t1",
			diffuse:      "Normal.dds",
			slots:        []modelViewerDirectTextureAssignment{assign(modelViewerSlotTextureRole(1), "Color.dds", modelViewerDNFTrue())},
			wantDiffuses: []string{"Color.dds"},
		},
		{
			name:          "real color diffuse stays",
			diffuse:       "Skin.dds",
			lightMaps:     []modelViewerDirectTextureAssignment{assign("light_map", "Color.dds", modelViewerDNFTrue())},
			wantDiffuses:  []string{"Skin.dds"},
			wantLightMaps: []string{"Color.dds"},
		},
		{
			name:          "linear replacement is a light map",
			diffuse:       "Normal.dds",
			lightMaps:     []modelViewerDirectTextureAssignment{assign("light_map", "Linear.dds", modelViewerDNFTrue())},
			wantDiffuses:  []string{"Normal.dds"},
			wantLightMaps: []string{"Linear.dds"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meshes := []modelViewerDirectMesh{
				{
					component:          "Body",
					textureAuthored:    true,
					textureDefaultFile: tt.diffuse,
					textureAssignments: append(
						[]modelViewerDirectTextureAssignment{assign("diffuse", tt.diffuse, modelViewerDNFTrue())},
						tt.lightMaps...),
					slotTextureCandidates: tt.slots,
				},
			}

			attachSlotBoundDiffuse(meshes, dir, false, false)

			var diffuses, lightMaps []string
			for _, assignment := range meshes[0].textureAssignments {
				switch assignment.role {
				case "diffuse":
					diffuses = append(diffuses, assignment.file)
				case "light_map":
					lightMaps = append(lightMaps, assignment.file)
				}
			}
			if !slices.Equal(diffuses, tt.wantDiffuses) || !slices.Equal(lightMaps, tt.wantLightMaps) ||
				meshes[0].textureDefaultFile != tt.wantDiffuses[0] {
				t.Fatalf(
					"default = %q, diffuses = %v, light maps = %v, want %v and %v",
					meshes[0].textureDefaultFile,
					diffuses,
					lightMaps,
					tt.wantDiffuses,
					tt.wantLightMaps,
				)
			}
		})
	}
}

func TestAttachSlotBoundDiffuseReplacesLinearPsT0WithSRGBSlot(t *testing.T) {
	const (
		bc7Linear = 98
		bc7SRGB   = 99
	)
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/C1.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))
	writeTextureFile(t, dir, "Textures/B1.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))
	writeTextureFile(t, dir, "Textures/A1.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))
	writeTextureFile(t, dir, "Textures/D1.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))

	mask := modelViewerDirectTextureAssignment{
		role: "diffuse", resource: "ResourceC1", file: "Textures/C1.dds", conditions: modelViewerDNFTrue(),
	}
	slot := func(slot int, name string) modelViewerDirectTextureAssignment {
		return modelViewerDirectTextureAssignment{
			role:       modelViewerSlotTextureRole(slot),
			resource:   "Resource" + name,
			file:       "Textures/" + name + ".dds",
			conditions: modelViewerDNFTrue(),
		}
	}
	for _, tt := range []struct {
		name       string
		component  string
		wwmi       bool
		authored   bool
		linearOnly bool
		want       string
	}{
		{name: "component", component: "Component7", want: "A1"},
		{name: "underscore component", component: "_Component7", want: "A1"},
		{name: "normal component", component: "NormalComponent3", wwmi: true, want: "A1"},
		{name: "UL component", component: "ULComponent3", wwmi: true, want: "A1"},
		{name: "non-WWMI prefix", component: "NormalComponent3", want: "C1"},
		{name: "authored diffuse", component: "NormalComponent3", wwmi: true, authored: true, want: "C1"},
		{name: "linear candidates", component: "NormalComponent3", wwmi: true, linearOnly: true, want: "C1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []modelViewerDirectTextureAssignment{slot(1, "B1")}
			if !tt.linearOnly {
				candidates = append(candidates, slot(4, "D1"), slot(2, "A1"))
			}
			meshes := []modelViewerDirectMesh{{
				component:             tt.component,
				textureAuthored:       tt.authored,
				textureAssignments:    []modelViewerDirectTextureAssignment{mask},
				textureDefaultFile:    mask.file,
				slotTextureCandidates: candidates,
			}}

			attachSlotBoundDiffuse(meshes, dir, tt.wwmi, false)

			mesh := meshes[0]
			want := "Textures/" + tt.want + ".dds"
			if mesh.textureDefaultFile != want || len(mesh.textureAssignments) != 1 ||
				mesh.textureAssignments[0].file != want {
				t.Fatalf(
					"default = %q, assignments = %#v, want %s",
					mesh.textureDefaultFile,
					mesh.textureAssignments,
					want,
				)
			}
		})
	}
}

func TestAttachSlotBoundDiffuseReadsWuWaSlotMaps(t *testing.T) {
	const (
		bc7Linear = 98
		bc7SRGB   = 99
	)
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/N.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))
	writeTextureFile(t, dir, "Textures/L.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))
	writeTextureFile(t, dir, "Textures/M.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))
	writeTextureFile(t, dir, "Textures/D.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))
	writeTextureFile(t, dir, "Textures/DLinear.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))

	assignment := func(role, name string) modelViewerDirectTextureAssignment {
		return modelViewerDirectTextureAssignment{
			role:       role,
			resource:   "Resource" + name,
			file:       "Textures/" + name + ".dds",
			conditions: modelViewerDNFTrue(),
		}
	}
	fullSet := ModelViewerDNF{{{Var: "shader", Value: "1"}}}
	otherShader := ModelViewerDNF{{{Var: "shader", Value: "2"}}}
	for _, tt := range []struct {
		name    string
		diffuse string
		// A diffuse that another shader branch binds alone, which never draws with ps-t3.
		branchDiffuse string
		want          map[string]string
	}{
		{
			name:    "sRGB ps-t3",
			diffuse: "D",
			want:    map[string]string{"normal_map": "N", "light_map": "L", "material_map": "M", "diffuse": "D"},
		},
		{
			name:          "other shader branch",
			diffuse:       "D",
			branchDiffuse: "L",
			want:          map[string]string{"normal_map": "N", "light_map": "L", "material_map": "M", "diffuse": "D"},
		},
		// A linear ps-t3 is no color map, so the slots keep their generic handling.
		{name: "linear ps-t3", diffuse: "DLinear", want: map[string]string{"diffuse": "N"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withConditions := func(assignment modelViewerDirectTextureAssignment, conditions ModelViewerDNF) modelViewerDirectTextureAssignment {
				if tt.branchDiffuse != "" {
					assignment.conditions = conditions
				}
				return assignment
			}
			normal := withConditions(assignment("diffuse", "N"), fullSet)
			assignments := []modelViewerDirectTextureAssignment{normal}
			if tt.branchDiffuse != "" {
				assignments = append(assignments, withConditions(assignment("diffuse", tt.branchDiffuse), otherShader))
			}
			meshes := []modelViewerDirectMesh{{
				component:          "Component0",
				textureAssignments: assignments,
				textureDefaultFile: normal.file,
				slotTextureCandidates: []modelViewerDirectTextureAssignment{
					withConditions(assignment(modelViewerSlotTextureRole(1), "L"), fullSet),
					withConditions(assignment(modelViewerSlotTextureRole(2), "M"), fullSet),
					withConditions(assignment(modelViewerSlotTextureRole(3), tt.diffuse), fullSet),
				},
			}}

			attachSlotBoundDiffuse(meshes, dir, true, false)

			mesh := meshes[0]
			got := make(map[string]string)
			var diffuses []string
			for _, assignment := range mesh.textureAssignments {
				name := strings.TrimSuffix(strings.TrimPrefix(assignment.file, "Textures/"), ".dds")
				if assignment.role == "diffuse" {
					diffuses = append(diffuses, name)
				}
				got[assignment.role] = name
			}
			wantDiffuses := []string{tt.want["diffuse"]}
			if tt.branchDiffuse != "" {
				wantDiffuses = []string{tt.branchDiffuse, tt.want["diffuse"]}
				got["diffuse"] = tt.want["diffuse"]
			}
			slices.Sort(diffuses)
			slices.Sort(wantDiffuses)
			if !maps.Equal(got, tt.want) || !slices.Equal(diffuses, wantDiffuses) ||
				mesh.textureDefaultFile != "Textures/"+tt.want["diffuse"]+".dds" {
				t.Fatalf(
					"roles = %v, diffuses = %v, default = %q, want %v and %v",
					got,
					diffuses,
					mesh.textureDefaultFile,
					tt.want,
					wantDiffuses,
				)
			}
		})
	}
}
