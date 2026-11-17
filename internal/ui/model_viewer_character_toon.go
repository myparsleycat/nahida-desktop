package ui

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// characterToonFiles holds each game's per-character ramps and material values by catalog id; see
// assets/character_toon/NOTICE.txt.
//
//go:embed assets/character_toon
var characterToonFiles embed.FS

// characterToonDir prefixes the bundled toon URL of a character's texture.
const characterToonDir = "character_toon/"

type characterToonMaterial struct {
	Role string `json:"role"`
	// Indices are the index counts of the stock meshes drawn with the material, which only EFMI records.
	Indices []int `json:"indices"`
	// Floats and Colors are the game material's values by Unity property name.
	Floats   map[string]float64    `json:"floats"`
	Colors   map[string][4]float64 `json:"colors"`
	Textures map[string]string     `json:"textures"`
}

type characterToonGame struct {
	Characters map[string]struct {
		Materials []characterToonMaterial `json:"materials"`
	} `json:"characters"`
}

// characterRampTextures names the material texture holding the diffuse ramp, for each game whose toon shader reads
// one.
var characterRampTextures = map[string]string{
	"gimi": "_PackedShadowRampTex",
	"srmi": "_DiffuseRampMultiTex",
	"efmi": "_RD",
}

var characterToonGames = func() map[string]func() (characterToonGame, error) {
	games := map[string]func() (characterToonGame, error){}
	for _, game := range []string{"gimi", "srmi", "zzmi", "efmi"} {
		games[game] = sync.OnceValues(func() (characterToonGame, error) {
			var data characterToonGame
			raw, err := characterToonFiles.ReadFile("assets/" + characterToonDir + game + ".json")
			if err == nil {
				err = json.Unmarshal(raw, &data)
			}
			if err != nil {
				return data, fmt.Errorf("read bundled %s character toon data: %w", game, err)
			}
			return data, nil
		})
	}
	return games
}()

// characterToonRamps are the diffuse ramps of a character's stock materials, as game texture names.
type characterToonRamps struct {
	game             string
	body, hair, face string
	// byIndexCount holds the ramp of each stock mesh by its index count, which a mod keeps for a part it does not
	// remodel.
	byIndexCount map[int]string
	// names lists every ramp once, with the texture request and its key that load it at the same index, which
	// index holds by name.
	names    []string
	requests []toonTextureRequest
	keys     []string
	index    map[string]int
}

// characterToon is a catalog character's bundled toon data; either part is nil when the game's toon shader reads
// none or the character has none.
type characterToon struct {
	ramps     *characterToonRamps
	materials *characterToonMaterials
}

func loadCharacterToon(game, id string) (characterToon, error) {
	game = strings.ToLower(game)
	load := characterToonGames[game]
	if load == nil {
		return characterToon{}, nil
	}
	data, err := load()
	if err != nil {
		return characterToon{}, err
	}
	entry, ok := data.Characters[id]
	if !ok {
		return characterToon{}, nil
	}
	// The ramps do not depend on the material values, so they stay usable when those cannot be packed.
	toon := characterToon{ramps: characterRampsOf(game, entry.Materials)}
	toon.materials, err = characterMaterialsOf(game, entry.Materials)
	if err != nil {
		return toon, fmt.Errorf("pack %s/%s character materials: %w", game, id, err)
	}
	return toon, nil
}

// characterRampsOf picks the hair and face ramps from those materials and, for the rest of the model, the ramp
// of the body or cloth materials that draw the most indices, else the first material's.
func characterRampsOf(game string, materials []characterToonMaterial) *characterToonRamps {
	texture := characterRampTextures[game]
	if texture == "" {
		return nil
	}
	ramps := &characterToonRamps{game: game}
	var order []string
	weights := map[string]int{}
	fallback := ""
	for _, material := range materials {
		name := material.Textures[texture]
		if name == "" {
			continue
		}
		weight := 1
		for _, count := range material.Indices {
			if ramps.byIndexCount == nil {
				ramps.byIndexCount = map[int]string{}
			}
			ramps.byIndexCount[count] = name
			weight += count
		}
		switch role := material.Role; {
		case role == "hair":
			ramps.hair = cmp.Or(ramps.hair, name)
		case role == "face":
			ramps.face = cmp.Or(ramps.face, name)
		case strings.HasPrefix(role, "body") || role == "cloth":
			if weights[name] == 0 {
				order = append(order, name)
			}
			weights[name] += weight
		default:
			fallback = cmp.Or(fallback, name)
		}
	}
	for _, name := range order {
		if ramps.body == "" || weights[name] > weights[ramps.body] {
			ramps.body = name
		}
	}
	ramps.body = cmp.Or(ramps.body, fallback)
	if ramps.body == "" && ramps.hair == "" && ramps.face == "" {
		return nil
	}
	ramps.listNames()
	return ramps
}

// url is the bundled toon URL of one of the ramps.
func (r *characterToonRamps) url(name string) string {
	if name == "" {
		return ""
	}
	return bundledToonScheme + characterToonDir + r.game + "/" + name + ".webp"
}

// listNames fills names, requests, keys and index. Star Rail's colour ramps are read as sRGB; EFMI's _RD holds
// coverage in alpha and tints only by its chroma.
func (r *characterToonRamps) listNames() {
	r.index = map[string]int{}
	add := func(name string) {
		if _, seen := r.index[name]; seen || name == "" {
			return
		}
		r.index[name] = len(r.names)
		request := toonTextureRequest{url: r.url(name), srgb: r.game == "srmi"}
		r.names, r.requests, r.keys = append(r.names, name), append(r.requests, request), append(r.keys, request.key())
	}
	add(r.body)
	add(r.hair)
	add(r.face)
	for _, name := range slices.Sorted(maps.Values(r.byIndexCount)) {
		add(name)
	}
}

// rampFor is the ramp a mesh draws with: the stock mesh's with the same index count, else the one of the part the
// mesh was marked as. Eyes take none.
func (r *characterToonRamps) rampFor(mesh *viewerMesh) string {
	if name, ok := r.byIndexCount[mesh.info.IndexCount]; ok {
		return name
	}
	switch {
	case mesh.toonEye:
		return ""
	case mesh.toonFace || mesh.efmiFace:
		return r.face
	case mesh.toonHair || mesh.efmiHead:
		return cmp.Or(r.hair, r.body)
	}
	return r.body
}
