package inject

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/elevated"
)

func TestPrepareLaunchKeepsVerifiedFilesLocked(t *testing.T) {
	t.Parallel()
	for _, mode := range []RuntimeMode{ModeXXMI, ModeLegacy} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "loader.dll")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			spec := LaunchSpec{Mode: mode, ProcessName: "game.exe", WorkDir: root, StartMethod: "Manual",
				Priority: "Normal", InjectMode: "Inject", TimeoutSeconds: 5, ModuleDLL: path}
			file := VerifiedFile{Path: path, SHA256: fileHash(t, path)}
			if mode == ModeXXMI {
				spec.LoaderDLL = file
			} else {
				spec.LegacyLoader = file
			}
			closeFiles, err := prepareLaunch(t.Context(), &spec)
			if err != nil {
				t.Fatal(err)
			}
			defer closeFiles()
			if err := os.WriteFile(path, []byte("replacement"), 0o600); err == nil {
				t.Fatal("verified launch file could be changed before use")
			}
			closeFiles()
			if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := prepareLaunch(t.Context(), &spec); err == nil {
				t.Fatal("changed file passed verification")
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("failed verification leaked a lock: %v", err)
			}
		})
	}
}

func TestLaunchReleasesFilesAfterValidationFailure(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"invalid", ""} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := filepath.Join(root, "not-a-dll.dll")
			if err := os.WriteFile(path, []byte("not a PE"), 0o600); err != nil {
				t.Fatal(err)
			}
			spec := LaunchSpec{Mode: ModeXXMI, ProcessName: "nahida-security-test-not-running.exe", WorkDir: root,
				StartMethod: method, Priority: "Normal", InjectMode: "Inject", TimeoutSeconds: 5, ModuleDLL: path,
				LoaderDLL: VerifiedFile{Path: path, SHA256: fileHash(t, path)}}
			if _, err := Launch(t.Context(), spec); err == nil {
				t.Fatal("invalid launch succeeded")
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("failed launch leaked a lock: %v", err)
			}
		})
	}
}

func TestHelperRejectsRemovedCommandAndUnknownFields(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{`{"customLaunchCmd":"never execute"}`, `{"unknown":true}`, `{} {}`} {
		_, err := HelperHandler().Run(t.Context(), elevated.Peer{}, json.RawMessage(payload))
		if !errors.Is(err, errInvalidHelperLaunch) {
			t.Fatalf("payload %s: %v", payload, err)
		}
	}
}

func TestLoadInjectorDLLRejectsSiblingDependency(t *testing.T) {
	t.Parallel()
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dependency, err := os.ReadFile(filepath.Join(system, "version.dll"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "nahida-audit-dependency.dll"), dependency, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "injector.dll")
	if err := os.WriteFile(path, dependencyProbeDLL(t), 0o600); err != nil {
		t.Fatal(err)
	}
	library, err := loadInjectorDLL(path)
	if err == nil {
		_ = library.Release()
		t.Fatal("injector loaded a dependency from its writable directory")
	}
	if !errors.Is(err, windows.ERROR_MOD_NOT_FOUND) {
		t.Fatalf("load: %v, want missing dependency", err)
	}
}

// This inert x64 DLL imports one Windows export through a non-system alias. It has no
// entry point or executable code, and needs no compiler or third-party binaries on CI.
func dependencyProbeDLL(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 1024)
	copy(data, "MZ")
	binary.LittleEndian.PutUint32(data[0x3c:], 0x80)
	copy(data[0x80:], "PE\x00\x00")
	var headers bytes.Buffer
	for _, value := range []any{
		pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1, SizeOfOptionalHeader: 240, Characteristics: 0x2022},
		pe.OptionalHeader64{Magic: 0x20b, ImageBase: 0x6fff00000000, SectionAlignment: 4096, FileAlignment: 512,
			MajorOperatingSystemVersion: 6, MajorSubsystemVersion: 6, SizeOfImage: 8192, SizeOfHeaders: 512,
			Subsystem: 2, SizeOfStackReserve: 1 << 20, SizeOfStackCommit: 4096,
			SizeOfHeapReserve: 1 << 20, SizeOfHeapCommit: 4096, NumberOfRvaAndSizes: 16,
			DataDirectory: [16]pe.DataDirectory{1: {VirtualAddress: 0x1000, Size: 40}}},
		pe.SectionHeader32{Name: [8]uint8{'.', 'r', 'd', 'a', 't', 'a'}, VirtualSize: 512, VirtualAddress: 0x1000,
			SizeOfRawData: 512, PointerToRawData: 512, Characteristics: 0xc0000040},
	} {
		if err := binary.Write(&headers, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	copy(data[0x84:], headers.Bytes())
	binary.LittleEndian.PutUint32(data[0x200:], 0x1080)
	binary.LittleEndian.PutUint32(data[0x20c:], 0x1040)
	binary.LittleEndian.PutUint32(data[0x210:], 0x1090)
	copy(data[0x240:], "nahida-audit-dependency.dll\x00")
	binary.LittleEndian.PutUint64(data[0x280:], 0x10a0)
	binary.LittleEndian.PutUint64(data[0x290:], 0x10a0)
	copy(data[0x2a2:], "GetFileVersionInfoSizeW\x00")
	return data
}
