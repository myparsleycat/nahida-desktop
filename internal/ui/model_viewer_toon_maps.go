package ui

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy/d3d"
)

// genshinToonMaps are Genshin's shared toon textures, which mods do not ship; see assets/genshin_toon/NOTICE.txt.
//
//go:embed assets/genshin_toon/*.png
var genshinToonMaps embed.FS

// bundledToonScheme prefixes the URL of a texture read from genshinToonMaps, or from characterToonFiles under
// characterToonDir, instead of the model viewer's resource protocol.
const bundledToonScheme = "embed:"

func bundledFaceLightmapURL(faceType string) string {
	if faceType == "" {
		return ""
	}
	return bundledToonScheme + "Avatar_" + strings.ToUpper(faceType[:1]) + faceType[1:] + "_Tex_FaceLightmap.png"
}

func readBundledToonMap(url string) ([]byte, error) {
	name, _ := strings.CutPrefix(url, bundledToonScheme)
	files, path := genshinToonMaps, "assets/genshin_toon/"+name
	if strings.HasPrefix(name, characterToonDir) {
		files, path = characterToonFiles, "assets/"+name
	}
	data, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundled toon map %s: %w", name, err)
	}
	return data, nil
}

// bundledToonMaps decodes the metal map and the face shadow, which every GIMI draw binds when the style selects
// no folder texture.
var bundledToonMaps = sync.OnceValues(func() ([2]*viewerTextureData, error) {
	var maps [2]*viewerTextureData
	for i, entry := range []struct {
		name   string
		format uint32
	}{
		{"Avatar_Tex_MetalMap.png", d3d.FORMAT_R8G8B8A8_UNORM_SRGB},
		{"Avatar_Tex_Face_Shadow.png", d3d.FORMAT_R8G8B8A8_UNORM},
	} {
		data, err := readBundledToonMap(entry.name)
		if err != nil {
			return maps, err
		}
		texture, err := decodeViewerImage(data)
		if err != nil {
			return maps, fmt.Errorf("decode bundled toon map %s: %w", entry.name, err)
		}
		texture.format, texture.generateMips = entry.format, false
		maps[i] = texture
	}
	return maps, nil
})
