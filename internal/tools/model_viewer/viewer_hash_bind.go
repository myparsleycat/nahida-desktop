package modelviewer

import (
	"encoding/binary"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var (
	hashTextureSuffixRE = regexp.MustCompile(`(?i)(Diffuse|NormalMap|LightMap|MaterialMap)$`)
	ibComponentDumpRE   = regexp.MustCompile(
		`(?i)(?:^|[/\\])([0-9a-f]{8})_(\d+)_([0-9a-f]{8})_Hash_(DiffuseMap|LightMap|NormalMap|MaterialMap)\.`,
	)
	ibComponentDumpRefRE = regexp.MustCompile(`(?i)(?:^|_)IB_([0-9a-f]{8})(?:_+[A-Za-z][A-Za-z0-9]*)*_+Component(\d+)$`)
	hashLightMapRE       = regexp.MustCompile(`(?i)hash_lightmap|_lightmap\.`)
	hashDiffuseRE        = regexp.MustCompile(`(?i)hash_diffusemap|diffuse`)
	hashImageExtRE       = regexp.MustCompile(`(?i)\.(dds|png|jpe?g)$`)
	ddsSRGBDXGI          = map[uint32]bool{29: true, 72: true, 75: true, 78: true, 91: true, 93: true, 99: true}
)

type ibComponentDumpFile struct {
	file   string
	ibHash string
	index  string
	role   string
}

type hashImageFile struct {
	file string
	cond ModelViewerDNF
}

type hashImageSlot struct {
	files []hashImageFile
	role  string
	vars  map[string]bool
	area  int
}

type familyRoleSection struct {
	name     string
	role     string
	hash     string
	priority int
	files    []hashImageFile
}

func attachFamilyAndStemTextures(
	meshes []modelViewerDirectMesh,
	sections []modINISection,
	resources []modelViewerResource,
	variables map[string]any,
) {
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	roleSections := collapseSharedHashRoleSections(collectFamilyRoleSections(sections, resourceMap, variables))
	byName := map[string]*familyRoleSection{}
	for index := range roleSections {
		section := &roleSections[index]
		byName[strings.ToLower(section.name)] = section
	}
	overrides := map[string]bool{}
	for _, section := range sections {
		if strings.EqualFold(section.Header, "TextureOverride") {
			overrides[strings.ToLower(section.Name)] = true
		}
	}
	for meshIndex := range meshes {
		mesh := &meshes[meshIndex]
		choices := familySiblingChoices(mesh, byName, overrides)
		lend := func(first int) {
			if len(choices) == 0 {
				return
			}
			for index := first; index < len(mesh.textureAssignments); index++ {
				mesh.textureAssignments[index].borrowed = true
			}
		}
		for _, role := range []string{"diffuse", "light_map", "normal_map", "material_map"} {
			if meshHasRole(mesh, role) {
				continue
			}
			suffix := familyRoleSuffix(role)
			first := len(mesh.textureAssignments)
			source := lookupFamilyRoleSection(byName, mesh.sectionName, suffix)
			if source != nil && len(source.files) > 0 {
				// familySiblingChoices offers choices only when neither the mesh's nor the family's section exists,
				// so the section is a sibling's.
				applyFamilyRoleFiles(mesh, source.files, role, true)
				lend(first)
				continue
			}
			if role != "diffuse" && role != "light_map" && role != "normal_map" && role != "material_map" {
				continue
			}
			if found := lookupRoleResource(mesh.ibName, suffix, resources); found != "" {
				file := resourceMap[modelViewerNormalizeKey(found)].Filename
				if file == "" {
					continue
				}
				applyFamilyRoleFiles(mesh, []hashImageFile{{file: file, cond: modelViewerDNFTrue()}}, role, true)
				if siblingRoleResource(mesh.ibName, suffix, found) {
					lend(first)
				}
			}
		}
		if len(choices) > 0 {
			mesh.textureChoices = choices
		}
	}
}

// familySiblingChoices returns the texture sets of the other parts of mesh's draw family when the mesh has no
// texture of its own and two or more siblings replace different textures. Such an INI replaces textures by hash, so
// it does not say which of them the game binds to the mesh.
func familySiblingChoices(
	mesh *modelViewerDirectMesh,
	byName map[string]*familyRoleSection,
	overrides map[string]bool,
) []modelViewerDirectTextureChoice {
	name := mesh.sectionName
	if len(mesh.textureAssignments) > 0 || mesh.textureDefaultFile != "" || mesh.textureAuthored ||
		name == "" || !isASCIIUpper(name[len(name)-1]) {
		return nil
	}
	family := name[:len(name)-1]
	roles := []string{"diffuse", "light_map", "normal_map", "material_map"}
	for _, role := range roles {
		suffix := familyRoleSuffix(role)
		if byName[strings.ToLower(name+suffix)] != nil || byName[strings.ToLower(family+suffix)] != nil {
			return nil
		}
	}

	var choices []modelViewerDirectTextureChoice
	seen := map[string]bool{}
	for letter := byte('A'); letter <= 'Z'; letter++ {
		sibling := family + string(letter)
		if sibling == name || !overrides[strings.ToLower(sibling)] {
			continue
		}
		diffuse := byName[strings.ToLower(sibling+familyRoleSuffix("diffuse"))]
		if diffuse == nil || len(diffuse.files) == 0 {
			continue
		}
		key := fmt.Sprint(diffuse.files)
		if seen[key] {
			continue
		}
		seen[key] = true

		var scratch modelViewerDirectMesh
		for _, role := range roles {
			if section := byName[strings.ToLower(sibling+familyRoleSuffix(role))]; section != nil {
				applyFamilyRoleFiles(&scratch, section.files, role, true)
			}
		}
		choices = append(
			choices,
			modelViewerDirectTextureChoice{source: sibling, assignments: scratch.textureAssignments},
		)
	}
	if len(choices) < 2 {
		return nil
	}
	return choices
}

// releaseBorrowedSiblingTextures empties the slots of a mesh whose only textures are those a sibling lent it while
// the INI leaves the choice between several siblings open. The binding passes ran with the lent textures in place,
// so none of them filled the mesh by a guess of its own. A mesh that gained a texture of its own keeps the lent ones
// and offers no choice.
func releaseBorrowedSiblingTextures(meshes []modelViewerDirectMesh) {
	for index := range meshes {
		mesh := &meshes[index]
		if len(mesh.textureChoices) == 0 {
			continue
		}
		if slices.ContainsFunc(mesh.textureAssignments, func(a modelViewerDirectTextureAssignment) bool {
			return !a.borrowed
		}) {
			mesh.textureChoices = nil
			continue
		}
		mesh.textureAssignments = nil
		mesh.textureDefaultFile, mesh.textureAuthored = "", false
	}
}

func normalizeModelViewerTextureAssignments(mesh *modelViewerDirectMesh, variables map[string]any) {
	normalize := func(assignments []modelViewerDirectTextureAssignment) {
		for index := range assignments {
			assignments[index].conditions = normalizeModelViewerDNFWithDomains(assignments[index].conditions, variables)
		}
	}
	normalize(mesh.textureAssignments)
	for _, choice := range mesh.textureChoices {
		normalize(choice.assignments)
	}
}

func collectFamilyRoleSections(
	sections []modINISection,
	resources map[string]modelViewerResource,
	variables map[string]any,
) []familyRoleSection {
	var output []familyRoleSection
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		match := hashTextureSuffixRE.FindStringSubmatch(section.Name)
		if match == nil {
			continue
		}
		role := familyRoleFromSuffix(match[1])
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil {
			continue
		}
		files := sectionReplacedImages(state, resources, func(resource modelViewerResource) bool {
			return resource.Filename != ""
		}, role)
		if len(files) == 0 {
			continue
		}
		meta := sectionMatchMeta(section)
		output = append(
			output,
			familyRoleSection{name: section.Name, role: role, hash: meta.hash, priority: meta.priority, files: files},
		)
	}
	return output
}

func collapseSharedHashRoleSections(sections []familyRoleSection) []familyRoleSection {
	groups := map[string][]int{}
	order := []string{}
	for index, section := range sections {
		if section.hash == "" {
			continue
		}
		key := section.role + ":" + section.hash
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], index)
	}
	for _, key := range order {
		indexes := groups[key]
		if len(indexes) < 2 {
			continue
		}
		winner := indexes[0]
		for _, index := range indexes[1:] {
			if sections[index].priority >= sections[winner].priority {
				winner = index
			}
		}
		for _, index := range indexes {
			if index == winner {
				continue
			}
			sections[index].files = append([]hashImageFile(nil), sections[winner].files...)
		}
	}
	return sections
}

func lookupFamilyRoleSection(byName map[string]*familyRoleSection, sectionName, suffix string) *familyRoleSection {
	if section := byName[strings.ToLower(sectionName+suffix)]; section != nil {
		return section
	}
	if sectionName == "" || !isASCIIUpper(sectionName[len(sectionName)-1]) {
		return nil
	}
	family := sectionName[:len(sectionName)-1]
	if section := byName[strings.ToLower(family+"A"+suffix)]; section != nil {
		return section
	}
	return byName[strings.ToLower(family+suffix)]
}

func lookupRoleResource(ibRes, role string, resources []modelViewerResource) string {
	if ibRes == "" {
		return ""
	}
	stem, family := roleResourceStem(ibRes)
	candidates := []string{stem + role}
	if family != stem {
		candidates = append(candidates, family+"A"+role, family+role)
	}
	for _, candidate := range candidates {
		for _, resource := range resources {
			if roleResourceNamed(resource.Name, candidate) {
				return resource.Name
			}
		}
	}
	return ""
}

// siblingRoleResource reports whether name, which lookupRoleResource found for ibRes, is the resource of the family's
// part A rather than the mesh's own or the family's.
func siblingRoleResource(ibRes, role, name string) bool {
	stem, family := roleResourceStem(ibRes)
	return family != stem && roleResourceNamed(name, family+"A"+role) && !roleResourceNamed(name, stem+role)
}

// roleResourceStem returns the resource name ibRes shares with its role resources, without its IB suffix and
// numbered copy suffix, and that name without its part letter.
func roleResourceStem(ibRes string) (stem, family string) {
	stem = ibRes
	if dot := strings.LastIndex(stem, "."); dot >= 0 && isAllDigits(stem[dot+1:]) {
		stem = stem[:dot]
	}
	if len(stem) >= 2 && strings.EqualFold(stem[len(stem)-2:], "IB") {
		stem = stem[:len(stem)-2]
	}
	family = stem
	if stem != "" && isASCIIUpper(stem[len(stem)-1]) {
		family = stem[:len(stem)-1]
	}
	return stem, family
}

func roleResourceNamed(name, candidate string) bool {
	lowered, want := strings.ToLower(name), strings.ToLower(candidate)
	return lowered == want || strings.HasPrefix(lowered, want+".")
}

func sectionMatchMeta(section modINISection) (meta struct {
	hash     string
	priority int
}) {
	for _, line := range section.Lines {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "hash":
			meta.hash = strings.ToLower(strings.TrimSpace(value))
		case "match_priority":
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				meta.priority = parsed
			}
		}
	}
	return meta
}

func applyFamilyRoleFiles(mesh *modelViewerDirectMesh, files []hashImageFile, role string, authored bool) {
	if mesh == nil || len(files) == 0 {
		return
	}
	keepVariants := len(files) > 1 || (len(files) == 1 && !modelViewerDNFIsTrue(files[0].cond))
	defaultFile := files[0].file
	for _, entry := range files {
		if modelViewerDNFIsTrue(entry.cond) {
			defaultFile = entry.file
			break
		}
	}
	if role == "diffuse" {
		if mesh.textureDefaultFile == "" {
			mesh.textureDefaultFile = defaultFile
		}
		if authored {
			mesh.textureAuthored = true
		}
	}
	if keepVariants {
		for _, entry := range files {
			mesh.textureAssignments = append(
				mesh.textureAssignments,
				modelViewerDirectTextureAssignment{
					role:       role,
					resource:   entry.file,
					file:       entry.file,
					authored:   authored,
					conditions: cloneModelViewerDNF(entry.cond),
				},
			)
		}
		return
	}
	mesh.textureAssignments = append(
		mesh.textureAssignments,
		modelViewerDirectTextureAssignment{
			role:       role,
			resource:   defaultFile,
			file:       defaultFile,
			authored:   authored,
			conditions: modelViewerDNFTrue(),
		},
	)
}

func meshHasRole(mesh *modelViewerDirectMesh, role string) bool {
	if mesh == nil {
		return false
	}
	if role == "diffuse" && (mesh.textureDefaultFile != "" || mesh.textureAuthored) {
		return true
	}
	for _, assignment := range mesh.textureAssignments {
		if assignment.role == role {
			return true
		}
	}
	return false
}

func familyRoleSuffix(role string) string {
	switch role {
	case "normal_map":
		return "NormalMap"
	case "light_map":
		return "LightMap"
	case "material_map":
		return "MaterialMap"
	default:
		return "Diffuse"
	}
}

func familyRoleFromSuffix(suffix string) string {
	switch strings.ToLower(suffix) {
	case "normalmap":
		return "normal_map"
	case "lightmap":
		return "light_map"
	case "materialmap":
		return "material_map"
	default:
		return "diffuse"
	}
}

func isASCIIUpper(value byte) bool {
	return value >= 'A' && value <= 'Z'
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func attachIbComponentDumpTextures(meshes []modelViewerDirectMesh, resources []modelViewerResource) {
	var dumps []ibComponentDumpFile
	for _, resource := range resources {
		file := resource.Filename
		match := ibComponentDumpRE.FindStringSubmatch(slashPath(file))
		if file == "" || match == nil {
			continue
		}
		dumps = append(
			dumps,
			ibComponentDumpFile{
				file:   file,
				ibHash: strings.ToLower(match[1]),
				index:  match[2],
				role:   strings.ToLower(match[4]),
			},
		)
	}
	if len(dumps) == 0 {
		return
	}
	for index := range meshes {
		mesh := &meshes[index]
		refHash, refIndex, ok := ibComponentDumpRef(mesh.component)
		if !ok {
			continue
		}
		var ibDumps []ibComponentDumpFile
		for _, dump := range dumps {
			if dump.ibHash == refHash {
				ibDumps = append(ibDumps, dump)
			}
		}
		assignIbDump(mesh, pickIbComponentDumpFile(ibDumps, refIndex, "diffusemap"), "diffuse")
		assignIbDump(mesh, pickIbComponentDumpFile(ibDumps, refIndex, "lightmap"), "light_map")
		assignIbDump(mesh, pickIbComponentDumpFile(ibDumps, refIndex, "normalmap"), "normal_map")
		assignIbDump(mesh, pickIbComponentDumpFile(ibDumps, refIndex, "materialmap"), "material_map")
	}
}

func pickIbComponentDumpFile(dumps []ibComponentDumpFile, index, role string) string {
	var roleDumps []ibComponentDumpFile
	for _, dump := range dumps {
		if dump.role == role {
			roleDumps = append(roleDumps, dump)
		}
	}
	for _, dump := range roleDumps {
		if dump.index == index {
			return dump.file
		}
	}
	unique := map[string]bool{}
	var files []string
	for _, dump := range roleDumps {
		if !unique[dump.file] {
			unique[dump.file] = true
			files = append(files, dump.file)
		}
	}
	if len(files) == 1 {
		return files[0]
	}
	return ""
}

func ibComponentDumpRef(name string) (string, string, bool) {
	match := ibComponentDumpRefRE.FindStringSubmatch(name)
	if match == nil {
		return "", "", false
	}
	return strings.ToLower(match[1]), match[2], true
}

func assignIbDump(mesh *modelViewerDirectMesh, file, role string) {
	if mesh == nil || file == "" {
		return
	}
	if role == "diffuse" && mesh.textureDefaultFile == "" {
		mesh.textureDefaultFile = file
	}
	for _, assignment := range mesh.textureAssignments {
		if assignment.role == role {
			return
		}
	}
	mesh.textureAssignments = append(mesh.textureAssignments, modelViewerDirectTextureAssignment{
		role:       role,
		resource:   file,
		file:       file,
		conditions: modelViewerDNFTrue(),
	})
}

func bindHashImageTextures(
	meshes []modelViewerDirectMesh,
	sections []modINISection,
	resources []modelViewerResource,
	modDir string,
	variables map[string]any,
) {
	// Authored components join only when no mesh carries any texture of its own:
	// the standalone hash replacements are then the mod's sole texture evidence.
	// Otherwise a leftover replacement more likely belongs to an undrawn part.
	// Dump-named textures stay with the dump binders, which match them by index.
	namedFallback := !slices.ContainsFunc(meshes, meshHasTextureEvidence) &&
		!slices.ContainsFunc(resources, isDumpTextureResource)
	var needed []*modelViewerDirectMesh
	for index := range meshes {
		mesh := &meshes[index]
		if _, _, ok := ibComponentDumpRef(mesh.component); !ok && !namedFallback {
			continue
		}
		if mesh.textureDefaultFile != "" {
			continue
		}
		needed = append(needed, mesh)
	}
	if len(needed) == 0 {
		return
	}

	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	slots := collectHashImageSlots(sections, resourceMap, variables, modDir)
	if len(slots) == 0 {
		return
	}

	var diffuseSlots []*hashImageSlot
	lightsByArea := make(map[int][]*hashImageSlot)
	for index := range slots {
		slot := &slots[index]
		switch slot.role {
		case "light_map":
			lightsByArea[slot.area] = append(lightsByArea[slot.area], slot)
		default:
			diffuseSlots = append(diffuseSlots, slot)
		}
	}
	paired := make(map[*hashImageSlot]*hashImageSlot)
	for _, slot := range diffuseSlots {
		if slot.area > 0 && len(lightsByArea[slot.area]) > 0 {
			paired[slot] = lightsByArea[slot.area][0]
			lightsByArea[slot.area] = lightsByArea[slot.area][1:]
		}
	}

	byIb := make(map[string][]*modelViewerDirectMesh)
	unbound := map[string]bool{}
	for _, mesh := range needed {
		ibHash, _, ok := ibComponentDumpRef(mesh.component)
		if !ok {
			ibHash = "ib:" + modelViewerNormalizeKey(mesh.ibName)
		}
		byIb[ibHash] = append(byIb[ibHash], mesh)
		unbound[ibHash] = true
	}
	unused := make(map[*hashImageSlot]bool, len(diffuseSlots))
	for _, slot := range diffuseSlots {
		unused[slot] = true
	}

	type scoredBind struct {
		ibHash        string
		slot          *hashImageSlot
		score         int
		tightness     int
		defaultActive bool
	}
	var scored []scoredBind
	for _, ibHash := range slices.Sorted(maps.Keys(byIb)) {
		groupVars := meshDNFVars(byIb[ibHash])
		for _, slot := range diffuseSlots {
			if score := overlapScore(groupVars, slot.vars); score > 0 {
				scored = append(scored, scoredBind{
					ibHash:    ibHash,
					slot:      slot,
					score:     score,
					tightness: len(slot.vars),
					defaultActive: slices.ContainsFunc(slot.files, func(entry hashImageFile) bool {
						return viewerEvalDNFSatisfied(entry.cond, variables)
					}),
				})
			}
		}
	}

	// A mod can override several game textures under the same toggles, such as a
	// body and its ultimate form. Nothing ties either to a draw, so remaining ties
	// fall to the default state and then to declaration order.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].tightness != scored[j].tightness {
			return scored[i].tightness < scored[j].tightness
		}
		if scored[i].slot.area != scored[j].slot.area {
			return scored[i].slot.area > scored[j].slot.area
		}
		return scored[i].defaultActive && !scored[j].defaultActive
	})
	for _, pick := range scored {
		if !unbound[pick.ibHash] || !unused[pick.slot] {
			continue
		}
		applyHashImageSlot(byIb[pick.ibHash], pick.slot, paired[pick.slot])
		delete(unbound, pick.ibHash)
		delete(unused, pick.slot)
	}

	var leftoverIbs []string
	for ibHash := range unbound {
		leftoverIbs = append(leftoverIbs, ibHash)
	}
	sort.Strings(leftoverIbs)
	sort.SliceStable(leftoverIbs, func(i, j int) bool {
		return ibDrawWeight(byIb[leftoverIbs[i]]) > ibDrawWeight(byIb[leftoverIbs[j]])
	})
	var leftoverSlots []*hashImageSlot
	for _, slot := range diffuseSlots {
		if unused[slot] {
			leftoverSlots = append(leftoverSlots, slot)
		}
	}
	sort.SliceStable(leftoverSlots, func(i, j int) bool {
		return leftoverSlots[i].area > leftoverSlots[j].area
	})

	// Nothing ties a leftover image to a draw, so the largest is only a guess. The slots that always bind a single
	// image stay open as candidates for the UV match that runs once the geometry is loaded. A moved component takes
	// only the file, so a condition on the image would be lost or left on the wrong one.
	unconditional := func(slot *hashImageSlot) bool {
		return len(slot.files) == 1 && modelViewerDNFIsTrue(slot.files[0].cond)
	}
	var candidates []hashAtlasCandidate
	for _, slot := range leftoverSlots {
		if !unconditional(slot) {
			continue
		}
		candidate := hashAtlasCandidate{diffuse: slot.files[0].file}
		if light := paired[slot]; light != nil && len(light.files) > 0 {
			candidate.light = light.files[0].file
		}
		candidates = append(candidates, candidate)
	}
	for index, ibHash := range leftoverIbs {
		if index >= len(leftoverSlots) {
			break
		}
		slot := leftoverSlots[index]
		applyHashImageSlot(byIb[ibHash], slot, paired[slot])
		guess := slices.IndexFunc(candidates, func(candidate hashAtlasCandidate) bool {
			return unconditional(slot) && candidate.diffuse == slot.files[0].file
		})
		if guess < 0 || len(candidates) < 2 {
			continue
		}
		ordered := slices.Concat(candidates[guess:guess+1], candidates[:guess], candidates[guess+1:])
		for _, mesh := range byIb[ibHash] {
			mesh.hashAtlasCandidates = ordered
		}
	}
}

func collectHashImageSlots(
	sections []modINISection,
	resources map[string]modelViewerResource,
	variables map[string]any,
	modDir string,
) []hashImageSlot {
	var slots []hashImageSlot
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") || hashTextureSuffixRE.MatchString(section.Name) ||
			sectionHasIBOrDraw(section) {
			continue
		}
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil || state.explicitDraw {
			continue
		}
		files := sectionThisImages(state, resources, isHashImageResource)
		if len(files) == 0 {
			continue
		}
		hint := hashImageHint(files[0].file, modDir)
		vars := map[string]bool{}
		for _, entry := range files {
			for _, name := range dnfVarNames(entry.cond) {
				vars[name] = true
			}
		}
		slots = append(slots, hashImageSlot{files: files, role: hint.role, vars: vars, area: hint.area})
	}
	return slots
}

// sectionThisImages lists the files of the resources a scanned section assigns to `this` that keep passes, once per
// file and condition.
func sectionThisImages(
	state *modelViewerSymbolicSectionState,
	resources map[string]modelViewerResource,
	keep func(modelViewerResource) bool,
) []hashImageFile {
	return symbolicImageFiles(effectiveModelViewerSymbolicAssignments(state.thisHistory), resources, keep)
}

// sectionReplacedImages lists the images a scanned hash override replaces its texture with for role: those it
// assigns to `this`, or else those it binds to ps-t slots under role, as mods that write the replacement as
// `ps-t0 = Resource` do. A section that binds an index or vertex buffer is a mesh override, so its slot bindings are
// not taken as a replacement.
func sectionReplacedImages(
	state *modelViewerSymbolicSectionState,
	resources map[string]modelViewerResource,
	keep func(modelViewerResource) bool,
	role string,
) []hashImageFile {
	if files := sectionThisImages(state, resources, keep); len(files) != 0 || len(state.buffers) != 0 {
		return files
	}
	var slotted []modelViewerSymbolicAssignment
	for _, assignment := range state.textures[role] {
		if assignment.textureSlot >= 0 {
			slotted = append(slotted, assignment)
		}
	}
	return symbolicImageFiles(effectiveModelViewerSymbolicAssignments(slotted), resources, keep)
}

func symbolicImageFiles(
	assignments []modelViewerSymbolicAssignment,
	resources map[string]modelViewerResource,
	keep func(modelViewerResource) bool,
) []hashImageFile {
	var files []hashImageFile
	seen := map[string]bool{}
	for _, assignment := range assignments {
		resource, ok := resources[modelViewerNormalizeKey(assignment.resource)]
		if !ok || !keep(resource) {
			continue
		}
		key := slashPath(resource.Filename) + "|" + modelViewerSymbolicDNFKey(assignment.conditions)
		if seen[key] {
			continue
		}
		seen[key] = true
		files = append(files, hashImageFile{file: resource.Filename, cond: cloneModelViewerDNF(assignment.conditions)})
	}
	return files
}

func isHashImageResource(resource modelViewerResource) bool {
	return hashImageExtRE.MatchString(resource.Filename)
}

func applyHashImageSlot(meshes []*modelViewerDirectMesh, slot *hashImageSlot, light *hashImageSlot) {
	if slot == nil || len(slot.files) == 0 {
		return
	}
	defaultFile := slot.files[0].file
	for _, entry := range slot.files {
		if modelViewerDNFIsTrue(entry.cond) {
			defaultFile = entry.file
			break
		}
	}
	keepVariants := len(slot.files) > 1 || (len(slot.files) == 1 && !modelViewerDNFIsTrue(slot.files[0].cond))
	lightFile := ""
	if light != nil && len(light.files) > 0 {
		lightFile = light.files[0].file
	}
	for _, mesh := range meshes {
		if mesh.textureDefaultFile == "" {
			mesh.textureDefaultFile = defaultFile
		}
		hasDiffuse := false
		for _, assignment := range mesh.textureAssignments {
			if assignment.role == "diffuse" {
				hasDiffuse = true
				break
			}
		}
		if !hasDiffuse {
			if keepVariants {
				for _, entry := range slot.files {
					mesh.textureAssignments = append(
						mesh.textureAssignments,
						modelViewerDirectTextureAssignment{
							role:       "diffuse",
							resource:   entry.file,
							file:       entry.file,
							conditions: cloneModelViewerDNF(entry.cond),
						},
					)
				}
			} else {
				mesh.textureAssignments = append(
					mesh.textureAssignments,
					modelViewerDirectTextureAssignment{
						role:       "diffuse",
						resource:   defaultFile,
						file:       defaultFile,
						conditions: modelViewerDNFTrue(),
					},
				)
			}
		}
		if lightFile != "" {
			hasLight := false
			for _, assignment := range mesh.textureAssignments {
				if assignment.role == "light_map" {
					hasLight = true
					break
				}
			}
			if !hasLight {
				mesh.textureAssignments = append(
					mesh.textureAssignments,
					modelViewerDirectTextureAssignment{
						role:       "light_map",
						resource:   lightFile,
						file:       lightFile,
						conditions: modelViewerDNFTrue(),
					},
				)
			}
		}
	}
}

func hashImageHint(relative, modDir string) (hint struct {
	role string
	area int
}) {
	lower := strings.ToLower(slashPath(relative))
	if hashLightMapRE.MatchString(lower) && !hashDiffuseRE.MatchString(lower) {
		return struct {
			role string
			area int
		}{role: "light_map", area: peekImageArea(relative, modDir)}
	}
	if hashDiffuseRE.MatchString(lower) || !strings.HasSuffix(lower, ".dds") || modDir == "" {
		return struct {
			role string
			area int
		}{role: "diffuse", area: peekImageArea(relative, modDir)}
	}
	if peeked := peekDdsRole(viewerResourcePath(modDir, relative)); peeked != nil {
		return *peeked
	}
	return struct {
		role string
		area int
	}{role: "diffuse", area: 0}
}

func peekImageArea(relative, modDir string) int {
	if modDir == "" || !strings.HasSuffix(strings.ToLower(relative), ".dds") {
		return 0
	}
	if peeked := peekDdsRole(viewerResourcePath(modDir, relative)); peeked != nil {
		return peeked.area
	}
	return 0
}

func peekDdsRole(filePath string) *struct {
	role string
	area int
} {
	if filePath == "" {
		return nil
	}
	header := readFilePrefix(filePath, 148)
	if len(header) < 128 || string(header[:4]) != "DDS " {
		return nil
	}
	area := int(binary.LittleEndian.Uint32(header[16:20]) * binary.LittleEndian.Uint32(header[12:16]))
	if string(header[84:88]) != "DX10" || len(header) < 132 {
		return &struct {
			role string
			area int
		}{role: "diffuse", area: area}
	}
	role := "light_map"
	if ddsSRGBDXGI[binary.LittleEndian.Uint32(header[128:132])] {
		role = "diffuse"
	}
	return &struct {
		role string
		area int
	}{role: role, area: area}
}

func meshHasTextureEvidence(mesh modelViewerDirectMesh) bool {
	return mesh.textureDefaultFile != "" || mesh.textureAuthored ||
		slices.ContainsFunc(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role != modelViewerCutoutMapRole
		}) ||
		len(mesh.slotTextureCandidates) > 0 || len(mesh.nonDiffuseTextureFiles) > 0
}

func isDumpTextureResource(resource modelViewerResource) bool {
	file := slashPath(resource.Filename)
	return ibComponentDumpRE.MatchString(file) || wwmiDumpTexRE.MatchString(file)
}

func sectionHasIBOrDraw(section modINISection) bool {
	for _, line := range section.Lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		normalized := modelViewerNormalizeKey(key)
		if normalized == "ib" || normalized == "drawindexed" {
			return true
		}
	}
	return false
}

func meshDNFVars(meshes []*modelViewerDirectMesh) map[string]bool {
	vars := map[string]bool{}
	for _, mesh := range meshes {
		for _, name := range dnfVarNames(mesh.conditions) {
			vars[name] = true
		}
	}
	return vars
}

func dnfVarNames(dnf ModelViewerDNF) []string {
	var names []string
	seen := map[string]bool{}
	for _, group := range dnf {
		for _, clause := range group {
			if clause.Var == "" || seen[clause.Var] {
				continue
			}
			seen[clause.Var] = true
			names = append(names, clause.Var)
		}
	}
	return names
}

func overlapScore(groupVars, slotVars map[string]bool) int {
	score := 0
	for slotVar := range slotVars {
		for groupVar := range groupVars {
			if groupVar == slotVar || strings.HasPrefix(groupVar, slotVar) || strings.HasPrefix(slotVar, groupVar) {
				score++
				break
			}
		}
	}
	return score
}

func ibDrawWeight(meshes []*modelViewerDirectMesh) int {
	sum := 0
	for _, mesh := range meshes {
		sum += mesh.indexCount
	}
	return sum
}

func modelViewerTextureKey(relative, role string) string {
	relative = slashPath(relative)
	if relative == "" || role == "" {
		return ""
	}
	return role + "::" + relative
}

func unionSlashPaths(existing []string, extra []string) []string {
	seen := map[string]bool{}
	var output []string
	for _, value := range append(append([]string(nil), existing...), extra...) {
		key := slashPath(value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		output = append(output, value)
	}
	return output
}
