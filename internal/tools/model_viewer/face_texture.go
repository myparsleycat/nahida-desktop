package modelviewer

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"nahida.live/desktop/internal/platform"
)

var faceTextureExcludeRE = regexp.MustCompile(`(?i)light|normal|material|shadow|sdf`)

// collectModelViewerFaceTextures returns the images of the first face-named texture override of an INI that replaces
// a game texture by hash without drawing, with the conditions under which each applies. Mods that leave the face mesh
// to the game replace its diffuse this way; an override whose image a draw of the INI already uses is left out.
func collectModelViewerFaceTextures(
	modDir string,
	sections []modINISection,
	resources []modelViewerResource,
	domains map[string]any,
	meshes []modelViewerDirectMesh,
	bindings []modelViewerTextureBinding,
) []hashImageFile {
	resourceMap := make(map[string]modelViewerResource, len(resources))
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	var used []string
	var variables map[string]any
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") || !modelViewerNameHasFace(section.Name) ||
			faceTextureExcludeRE.MatchString(section.Name) || sectionHasIBOrDraw(section) ||
			sectionMatchMeta(section).hash == "" {
			continue
		}
		if match := hashTextureSuffixRE.FindStringSubmatch(section.Name); match != nil &&
			familyRoleFromSuffix(match[1]) != "diffuse" {
			continue
		}
		if variables == nil {
			defaults := collectModelViewerDefaultVariables(sections)
			maps.Copy(defaults, domains)
			variables = modelViewerDirectConditionVariables(sections, defaults)
			used = modelViewerUsedTextureFiles(modDir, meshes, bindings, resourceMap)
		}
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil || state.explicitDraw {
			continue
		}

		files := sectionReplacedImages(state, resourceMap, isHashImageResource, "diffuse")
		claimed := slices.ContainsFunc(files, func(entry hashImageFile) bool {
			file := modelViewerFacePath(modDir, entry.file)
			return slices.ContainsFunc(used, func(other string) bool { return platform.SamePathFold(other, file) })
		})
		if len(files) == 0 || claimed {
			continue
		}
		if !hashTextureSuffixRE.MatchString(section.Name) && hashImageHint(files[0].file, modDir).role != "diffuse" {
			continue
		}
		for index := range files {
			files[index].cond = normalizeModelViewerDNFWithDomains(files[index].cond, variables)
		}
		return files
	}
	return nil
}

// modelViewerNameHasFace reports whether "face" is a word of name, so "FurinaFaceHeadDiffuse", "face_diffuse" and
// "CharFACEDiffuse" have it while "WaterSurfaceDiffuse" and "FacelessDiffuse" do not.
func modelViewerNameHasFace(name string) bool {
	return slices.ContainsFunc(modelViewerNameWords(name), func(word string) bool {
		return strings.EqualFold(word, "face")
	})
}

// modelViewerNameWords splits name into the letter runs between separators and camel-case boundaries. A run of
// capitals keeps its last one for the word that follows, so "FACEDiffuse" is "FACE" and "Diffuse".
func modelViewerNameWords(name string) []string {
	isUpper := func(c byte) bool { return c >= 'A' && c <= 'Z' }
	isLower := func(c byte) bool { return c >= 'a' && c <= 'z' }
	var words []string
	start := -1
	for i := 0; i <= len(name); i++ {
		if i == len(name) || !isUpper(name[i]) && !isLower(name[i]) {
			if start >= 0 {
				words = append(words, name[start:i])
			}
			start = -1
			continue
		}
		boundary := i > start && start >= 0 && isUpper(name[i]) &&
			(isLower(name[i-1]) || i+1 < len(name) && isLower(name[i+1]) && isUpper(name[i-1]))
		if boundary {
			words = append(words, name[start:i])
			start = i
		}
		if start < 0 {
			start = i
		}
	}
	return words
}

// modelViewerFacePath resolves a resource file of the mod to a path, lexically.
func modelViewerFacePath(modDir, file string) string {
	return filepath.Join(modDir, filepath.FromSlash(slashPath(file)))
}

// modelViewerUsedTextureFiles lists the paths of the files the meshes take as textures and the draws bind to shader
// slots.
func modelViewerUsedTextureFiles(
	modDir string,
	meshes []modelViewerDirectMesh,
	bindings []modelViewerTextureBinding,
	resources map[string]modelViewerResource,
) []string {
	var used []string
	add := func(file string) {
		if file != "" {
			used = append(used, modelViewerFacePath(modDir, file))
		}
	}
	addResource := func(name string) {
		if resource, ok := resources[modelViewerNormalizeKey(name)]; ok {
			add(resource.Filename)
		}
	}
	for _, mesh := range meshes {
		add(mesh.textureDefaultFile)
		for _, assignment := range mesh.textureAssignments {
			add(assignment.file)
			addResource(assignment.resource)
		}
	}
	for _, binding := range bindings {
		addResource(binding.DiffuseResourceName)
		for _, name := range binding.TextureResourceNames {
			addResource(name)
		}
	}
	return used
}

// modelViewerFaceTextureAssignments turns face images into diffuse assignments, so that texture jobs load them the
// way they load mesh textures.
func modelViewerFaceTextureAssignments(files []hashImageFile) []modelViewerDirectTextureAssignment {
	assignments := make([]modelViewerDirectTextureAssignment, len(files))
	for index, entry := range files {
		assignments[index] = modelViewerDirectTextureAssignment{
			role: "diffuse", resource: entry.file, file: entry.file, conditions: entry.cond,
		}
	}
	return assignments
}

// buildModelViewerFaceTexture resolves the face images to the keys of the loaded textures, or reports false when none
// loaded.
func buildModelViewerFaceTexture(
	form string,
	files []hashImageFile,
	textures map[string]modelViewerTexturePayload,
) (ModelViewerFaceTexture, bool) {
	face := ModelViewerFaceTexture{Form: form, Variants: []ModelViewerTextureVariant{}}
	for _, assignment := range modelViewerFaceTextureAssignments(files) {
		texture, ok := textures[modelViewerAssignmentTextureKey(assignment)]
		if !ok || texture.Key == "" {
			continue
		}
		if modelViewerDNFIsTrue(assignment.conditions) {
			face.TexKey = &texture.Key
			continue
		}
		face.Variants = append(
			face.Variants,
			ModelViewerTextureVariant{Conditions: assignment.conditions, TexKey: texture.Key},
		)
	}
	return face, face.TexKey != nil || len(face.Variants) > 0
}
