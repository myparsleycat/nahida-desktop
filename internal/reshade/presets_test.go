package reshade

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPresetEffectsReportMissingPackages(t *testing.T) {
	t.Parallel()
	list := `[00]
PackageName=One
InstallPath=.\reshade-shaders\Shaders
TextureInstallPath=.\reshade-shaders\Textures
DownloadUrl=https://github.com/owner/one/archive/main.zip
EffectFiles=Shared.fx

[01]
PackageName=Two
InstallPath=.\reshade-shaders\Shaders
TextureInstallPath=.\reshade-shaders\Textures
DownloadUrl=https://github.com/owner/two/archive/main.zip
EffectFiles=needed.fx,Shared.fx
`
	service := effectServiceWithTransport(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: r,
			Body: io.NopCloser(bytes.NewReader([]byte(list))), ContentLength: int64(len(list)),
		}, nil
	}))
	root := t.TempDir()
	service.root = func(context.Context) (string, error) { return root, nil }
	layout := layout{root: filepath.Join(root, "ReShade")}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names := func(effects PresetEffects) []string {
		found := []string{}
		for _, pkg := range effects.Packages {
			found = append(found, pkg.Name)
		}
		return found
	}
	write(filepath.Join(layout.shaders(), "Nested", "present.fx"), "shader")
	write(filepath.Join(layout.presets(), "My.ini"), "keep")

	// Shared.fx is in both packages and must not pull in One when Two is needed anyway.
	source := filepath.Join(t.TempDir(), "My.ini")
	write(source, string(rune(0xFEFF))+"Techniques=A@Present.fx,B@Needed.fx,C@Shared.fx,D@Nowhere.fx,Bare\r\n"+
		"[Needed.fx]\r\nTechniques=X@Ignored.fx\r\n")
	effects, err := service.AddPresets(t.Context(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(effects); !reflect.DeepEqual(got, []string{"Two"}) ||
		!reflect.DeepEqual(effects.Unknown, []string{"Nowhere.fx"}) {
		t.Fatalf("added preset effects = %v, unknown %v", got, effects.Unknown)
	}
	if got, err := os.ReadFile(filepath.Join(layout.presets(), "My.ini")); err != nil || string(got) != "keep" {
		t.Fatalf("existing preset = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(layout.presets(), "My (2).ini")); err != nil {
		t.Fatalf("added preset: %v", err)
	}

	write(filepath.Join(layout.root, "GIMI", "ReShade.ini"), "[GENERAL]\r\nPresetPath=.\\Custom.ini\r\n")
	write(filepath.Join(layout.root, "GIMI", "Custom.ini"), "Techniques=B@Needed.fx\r\n")
	write(filepath.Join(layout.root, "GIMI", defaultPreset), "Techniques=D@Nowhere.fx\r\n")
	effects, err = service.LaunchPresetEffects(t.Context(), "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(effects); !reflect.DeepEqual(got, []string{"Two"}) || len(effects.Unknown) != 0 {
		t.Fatalf("launch preset effects = %v, unknown %v", got, effects.Unknown)
	}
}
