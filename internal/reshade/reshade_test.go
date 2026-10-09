package reshade

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// peImage builds the smallest image debug/pe accepts: a DOS stub pointing at a bare COFF header.
func peImage(t *testing.T, machine, characteristics uint16) []byte {
	t.Helper()
	image := make([]byte, 0x40, 0x80)
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:], 0x40)
	image = append(image, 'P', 'E', 0, 0)
	header := pe.FileHeader{Machine: machine, Characteristics: characteristics}
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}
	image = append(image, buffer.Bytes()...)

	// debug/pe reads a fixed 96-byte DOS header before following the offset.
	return append(image, make([]byte, 64)...)
}

// setupFile writes a stub executable with an archive appended, the way the official setup ships.
func setupFile(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	var archive bytes.Buffer
	archive.WriteString("MZ setup stub")
	writer := zip.NewWriter(&archive)
	writer.SetOffset(int64(archive.Len()))
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "setup.exe")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractModuleTakesOnlyTheNamedEntry(t *testing.T) {
	t.Parallel()
	module := []byte("64-bit module")
	setup := setupFile(t, map[string][]byte{
		"ReShade32.dll":        []byte("32-bit module"),
		`..\ReShade64.dll`:     []byte("escaping entry"),
		"nested/ReShade64.dll": []byte("nested entry"),
		moduleName:             module,
	})
	destination := filepath.Join(t.TempDir(), moduleName)
	if err := extractModule(setup, destination); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || !bytes.Equal(got, module) {
		t.Fatalf("extracted module = %q, %v", got, err)
	}

	without := setupFile(t, map[string][]byte{"ReShade32.dll": []byte("32-bit module"), "reshade64.dll.bak": nil})
	if err := extractModule(without, filepath.Join(t.TempDir(), moduleName)); err == nil {
		t.Fatal("setup without the 64-bit module was accepted")
	}
}

func TestCheckModuleRejectsWrongBinary(t *testing.T) {
	t.Parallel()
	const dll = 0x2000
	cases := []struct {
		name            string
		machine         uint16
		characteristics uint16
		fileVersion     string
		ok              bool
	}{
		{"matching", pe.IMAGE_FILE_MACHINE_AMD64, dll, "6.8.0", true},
		{"x86", pe.IMAGE_FILE_MACHINE_I386, dll, "6.8.0", false},
		{"executable", pe.IMAGE_FILE_MACHINE_AMD64, 0, "6.8.0", false},
		{"other version", pe.IMAGE_FILE_MACHINE_AMD64, dll, "6.7.3", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), moduleName)
			if err := os.WriteFile(path, peImage(t, tc.machine, tc.characteristics), 0o600); err != nil {
				t.Fatal(err)
			}
			service := New(Options{})
			service.fileVersion = func(string) (string, error) { return tc.fileVersion, nil }
			if err := service.checkModule(path, "6.8.0"); (err == nil) != tc.ok {
				t.Fatalf("checkModule = %v; want ok = %t", err, tc.ok)
			}
		})
	}
}

func TestEffectPackagesOutsideSharedFolderAreUnsupported(t *testing.T) {
	t.Parallel()
	layout := layout{root: filepath.Join(t.TempDir(), "ReShade")}
	list := strings.Join([]string{
		"\ufeff[00]",
		"Enabled=1",
		"PackageName=Standard",
		`InstallPath=.\reshade-shaders\Shaders`,
		`TextureInstallPath=.\reshade-shaders\Textures`,
		"DownloadUrl=https://github.com/crosire/reshade-shaders/archive/slim.zip",
		"DenyEffectFiles=Blending.fx, Demo.fx",
		"[01]",
		"PackageName=Escaping",
		`InstallPath=.\reshade-shaders\..\..\Windows`,
		`TextureInstallPath=.\reshade-shaders\Textures`,
		"DownloadUrl=https://github.com/owner/repo/archive/main.zip",
		"[02]",
		"PackageName=Absolute",
		`InstallPath=C:\Windows\System32`,
		`TextureInstallPath=.\reshade-shaders\Textures`,
		"DownloadUrl=https://github.com/owner/repo/archive/main.zip",
		"[03]",
		"PackageName=Elsewhere",
		`InstallPath=.\reshade-shaders\Shaders\Elsewhere`,
		`TextureInstallPath=.\reshade-shaders\Textures`,
		"DownloadUrl=https://example.com/owner/repo/archive/main.zip",
		"[04]",
		"PackageName=Lookalike",
		`InstallPath=.\reshade-shaders\Shaders`,
		`TextureInstallPath=.\reshade-shaders\Textures`,
		"DownloadUrl=https://github.com.example.com/owner/repo/archive/main.zip",
	}, "\r\n")

	packages := parseEffectPackages([]byte(list), layout)
	if len(packages) != 5 {
		t.Fatalf("parsed %d packages; want 5", len(packages))
	}
	standard := packages[0]
	if !standard.Supported || !standard.Recommended || standard.installPath != layout.shaders() ||
		standard.textureInstallPath != layout.textures() || len(standard.denied) != 2 {
		t.Fatalf("standard package = %+v", standard)
	}
	for _, pkg := range packages[1:] {
		if pkg.Supported {
			t.Errorf("package %q was accepted: %+v", pkg.Name, pkg)
		}
	}
}
