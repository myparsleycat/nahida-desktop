package modelviewer

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// GenshinToonAssetsTransport lists the shared Genshin Impact textures found in
// a folder the user extracted them to. The application bundles none of them.
type GenshinToonAssetsTransport struct {
	MetalMapURL   string                     `json:"metalMapUrl,omitempty"`
	FaceShadowURL string                     `json:"faceShadowUrl,omitempty"`
	FaceLightmaps map[string]string          `json:"faceLightmaps"`
	Ramps         []GenshinToonRampTransport `json:"ramps"`
}

// GenshinToonRampTransport is one character's shadow ramp pair. Name is the
// file name prefix, such as Avatar_Loli_Catalyst_Nahida.
type GenshinToonRampTransport struct {
	Name    string `json:"name"`
	BodyURL string `json:"bodyUrl,omitempty"`
	HairURL string `json:"hairUrl,omitempty"`
}

var (
	genshinFaceLightmapRE = regexp.MustCompile(`(?i)^avatar_(boy|girl|lady|loli|male)_tex_facelightmap\.png$`)
	genshinPartRampRE     = regexp.MustCompile(`(?i)^(.+)_tex_(body|hair)_shadow_ramp\.png$`)
	genshinRampRE         = regexp.MustCompile(`(?i)^(.+)_shadow_ramp\.png$`)
)

func (t *Service) ListGenshinToonAssets(ctx context.Context, dir string) (GenshinToonAssetsTransport, error) {
	assets, err := t.listGenshinToonAssets(ctx, dir)
	if err != nil && !infra.IsCancellationError(err) {
		err = infra.ReportError(t.log, err, "ModelViewer.listGenshinToonAssets", infra.Diagnostic{
			Operation: "list-genshin-toon-assets",
			Fields:    map[string]any{"path": dir},
		})
	}
	return assets, err
}

// listGenshinToonAssets reads the folder and its direct subfolders, so both a
// flat extraction and one split into Avatar and character folders work. The
// first file found for a name wins.
func (t *Service) listGenshinToonAssets(ctx context.Context, dir string) (GenshinToonAssetsTransport, error) {
	assets := GenshinToonAssetsTransport{FaceLightmaps: map[string]string{}, Ramps: []GenshinToonRampTransport{}}
	release, err := diskio.AcquireDir(ctx, dir)
	if err != nil {
		return assets, err
	}
	defer release()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return assets, err
	}
	dirs := []string{dir}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}

	ramps := map[string]*GenshinToonRampTransport{}
	for index, folder := range dirs {
		if err := ctx.Err(); err != nil {
			return assets, err
		}
		if index > 0 {
			if entries, err = os.ReadDir(folder); err != nil {
				continue
			}
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			url := t.protocol.LocalFileURL(filepath.Join(folder, name), true)
			switch lower := strings.ToLower(name); {
			case lower == "avatar_tex_metalmap.png":
				assets.MetalMapURL = cmp.Or(assets.MetalMapURL, url)
			case lower == "avatar_tex_face_shadow.png":
				assets.FaceShadowURL = cmp.Or(assets.FaceShadowURL, url)
			case genshinFaceLightmapRE.MatchString(name):
				bodyType := strings.ToLower(genshinFaceLightmapRE.FindStringSubmatch(name)[1])
				assets.FaceLightmaps[bodyType] = cmp.Or(assets.FaceLightmaps[bodyType], url)
			case genshinPartRampRE.MatchString(name):
				match := genshinPartRampRE.FindStringSubmatch(name)
				ramp := genshinRamp(ramps, match[1])
				if strings.EqualFold(match[2], "hair") {
					ramp.HairURL = cmp.Or(ramp.HairURL, url)
				} else {
					ramp.BodyURL = cmp.Or(ramp.BodyURL, url)
				}
			case genshinRampRE.MatchString(name):
				ramp := genshinRamp(ramps, genshinRampRE.FindStringSubmatch(name)[1])
				ramp.BodyURL = cmp.Or(ramp.BodyURL, url)
			}
		}
	}

	for _, ramp := range ramps {
		assets.Ramps = append(assets.Ramps, *ramp)
	}
	less := platform.NewLocaleLess()
	slices.SortFunc(assets.Ramps, func(a, b GenshinToonRampTransport) int {
		switch {
		case less(a.Name, b.Name):
			return -1
		case less(b.Name, a.Name):
			return 1
		}
		return 0
	})
	return assets, nil
}

func genshinRamp(ramps map[string]*GenshinToonRampTransport, name string) *GenshinToonRampTransport {
	key := strings.ToLower(name)
	if ramps[key] == nil {
		ramps[key] = &GenshinToonRampTransport{Name: name}
	}
	return ramps[key]
}
