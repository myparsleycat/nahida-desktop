package xxmi

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	customDLLPackage = "xxmi-custom-dll"
	// customDLLName is the only runtime file a user may replace; the loader and compiler stay signed.
	customDLLName      = "d3d11.dll"
	customDLLMetadata  = "source.json"
	customDLLSizeLimit = 64 << 20
	sharedCustomDLLKey = "xxmi_custom_dll"
)

// CustomDLL describes a user-provided d3d11.dll copied into the package cache.
type CustomDLL struct {
	// ID is the first 12 hex digits of SHA256 and names the cache folder.
	ID string `json:"id"`
	// Name is the file name the user picked; the cached copy is always stored as d3d11.dll.
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	ImportedAt string `json:"importedAt"`
}

// customRuntimeDLL is the custom d3d11.dll a deployment writes instead of the signed one.
type customRuntimeDLL struct {
	id   string
	data []byte
}

// ImportCustomDLL copies a user-selected d3d11.dll into the package cache and returns its entry.
// The copy is not used until an importer or the shared setting references its ID.
func (x *XXMI) ImportCustomDLL(ctx context.Context, path string) (dll CustomDLL, returnErr error) {
	stage := "resolve-cache"
	root := ""
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.ImportCustomDLL", infra.Diagnostic{
			Operation: "import-custom-dll", Stage: stage,
			Fields: map[string]any{
				"source_path": installDiagnosticValue(path), "cache_root": root, "custom_dll": dll.ID,
			},
		})
	}()
	root, err := xxmiCacheRoot()
	if err != nil {
		return CustomDLL{}, err
	}

	stage = "import"
	x.customDLLMu.Lock()
	defer x.customDLLMu.Unlock()
	dll, err = importCustomDLL(ctx, root, strings.TrimSpace(path))
	if err != nil {
		return CustomDLL{}, err
	}
	if x.importedCustomDLLs == nil {
		x.importedCustomDLLs = map[string]bool{}
	}
	x.importedCustomDLLs[dll.ID] = true
	if x.log != nil {
		x.log.Info(map[string]any{"custom_dll": dll.ID, "name": dll.Name, "size": dll.Size}, "XXMI.ImportCustomDLL")
	}
	return dll, nil
}

// SetSharedCustomDLL selects the custom d3d11.dll for importers that follow the shared XXMI libraries.
// An empty ID returns them to the signed DLL.
func (x *XXMI) SetSharedCustomDLL(ctx context.Context, id string) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	x.customDLLMu.Lock()
	defer x.customDLLMu.Unlock()

	id = strings.TrimSpace(id)
	if id != "" {
		if err := x.requireCustomDLL(id); err != nil {
			return err
		}
	}
	if err := client.Settings.Upsert(ctx, sharedCustomDLLKey, &id); err != nil {
		return err
	}
	x.reportCleanup(x.pruneCustomDLLsLocked(ctx), "prune-custom-dll")
	return nil
}

// customDLLID returns the custom d3d11.dll an importer deploys, or "" when it uses the signed one.
// A custom DLL only applies in unsafe mode, which is what lets a launch accept an unsigned runtime file.
func (x *XXMI) customDLLID(ctx context.Context, cfg ImporterConfig) (string, error) {
	if cfg.Mode != RuntimeXXMI || !cfg.Migoto.UnsafeMode {
		return "", nil
	}
	if cfg.XXMIVersion.Follow != followShared {
		return cfg.CustomDLL, nil
	}
	client, err := x.settingsClient()
	if err != nil {
		return "", err
	}
	shared, err := client.Settings.GetValue(ctx, sharedCustomDLLKey)
	if err != nil || shared == nil {
		return "", err
	}
	return strings.TrimSpace(*shared), nil
}

func (x *XXMI) requireCustomDLL(id string) error {
	if !isCustomDLLID(id) {
		return fmt.Errorf("invalid custom DLL %q", id)
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	if _, err := readCustomDLLInfo(root, id); err != nil {
		return fmt.Errorf("XXMI_CUSTOM_DLL_MISSING: %w", err)
	}
	return nil
}

// pruneCustomDLLsLocked removes unreferenced DLLs from previous sessions. Imports in this session may still
// belong to a pending renderer request or an unsaved draft. The caller must hold customDLLMu.
func (x *XXMI) pruneCustomDLLsLocked(ctx context.Context) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	referenced := map[string]bool{}
	shared, err := client.Settings.GetValue(ctx, sharedCustomDLLKey)
	if err != nil {
		return err
	}
	if shared != nil {
		referenced[strings.TrimSpace(*shared)] = true
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return err
		}
		referenced[cfg.CustomDLL] = true
	}

	parent := filepath.Join(root, "packages", customDLLPackage)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isCustomDLLID(entry.Name()) || referenced[entry.Name()] ||
			x.importedCustomDLLs[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func importCustomDLL(ctx context.Context, root, path string) (CustomDLL, error) {
	if err := ctx.Err(); err != nil {
		return CustomDLL{}, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".dll") {
		return CustomDLL{}, errors.New("XXMI_CUSTOM_DLL_INVALID: not a .dll file")
	}
	file, err := os.Open(path)
	if err != nil {
		return CustomDLL{}, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return CustomDLL{}, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > customDLLSizeLimit {
		return CustomDLL{}, errors.New("XXMI_CUSTOM_DLL_INVALID: not a regular file or too large")
	}
	data, err := io.ReadAll(io.LimitReader(file, customDLLSizeLimit+1))
	if err != nil {
		return CustomDLL{}, err
	}
	if len(data) > customDLLSizeLimit {
		return CustomDLL{}, errors.New("XXMI_CUSTOM_DLL_INVALID: not a regular file or too large")
	}
	if err := validateCustomDLLImage(data); err != nil {
		return CustomDLL{}, err
	}

	hash := hashBytes(data)
	id := hash[:12]
	if existing, _, err := readCustomDLL(root, id); err == nil {
		return existing, nil
	}
	parent := filepath.Join(root, "packages", customDLLPackage)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return CustomDLL{}, err
	}
	staging, err := os.MkdirTemp(parent, id+".tmp-")
	if err != nil {
		return CustomDLL{}, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	dll := CustomDLL{
		ID: id, Name: filepath.Base(path), SHA256: hash, Size: int64(len(data)),
		ImportedAt: time.Now().UTC().Format(time.RFC3339),
	}
	metadata, err := json.MarshalIndent(dll, "", "  ")
	if err != nil {
		return CustomDLL{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, customDLLName), data, 0o600); err != nil {
		return CustomDLL{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, customDLLMetadata), metadata, 0o600); err != nil {
		return CustomDLL{}, err
	}

	// An entry that failed the read above is damaged. It is only a copy of the file being imported again.
	destination := filepath.Join(parent, id)
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return CustomDLL{}, errors.New("custom DLL cache is not a regular directory")
		}
		if err := os.RemoveAll(destination); err != nil {
			return CustomDLL{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return CustomDLL{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return CustomDLL{}, err
	}
	return dll, nil
}

// validateCustomDLLImage rejects files the 64-bit games cannot load as d3d11.dll.
func validateCustomDLLImage(data []byte) error {
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("XXMI_CUSTOM_DLL_INVALID: %w", err)
	}
	defer func() { _ = image.Close() }()
	if image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || image.Characteristics&pe.IMAGE_FILE_DLL == 0 {
		return errors.New("XXMI_CUSTOM_DLL_INVALID: not a 64-bit DLL")
	}
	return nil
}

func isCustomDLLID(id string) bool {
	return len(id) == 12 && strings.Trim(id, "0123456789abcdef") == ""
}

func readCustomDLLInfo(root, id string) (CustomDLL, error) {
	if !isCustomDLLID(id) {
		return CustomDLL{}, fmt.Errorf("invalid custom DLL %q", id)
	}
	data, err := os.ReadFile(filepath.Join(root, "packages", customDLLPackage, id, customDLLMetadata))
	if err != nil {
		return CustomDLL{}, err
	}
	var dll CustomDLL
	if err := json.Unmarshal(data, &dll); err != nil {
		return CustomDLL{}, err
	}
	if dll.ID != id || !strings.HasPrefix(dll.SHA256, id) {
		return CustomDLL{}, errors.New("custom DLL metadata mismatch")
	}
	return dll, nil
}

// readCustomDLL returns a cached custom DLL after checking its bytes against the recorded hash.
func readCustomDLL(root, id string) (CustomDLL, []byte, error) {
	dll, err := readCustomDLLInfo(root, id)
	if err != nil {
		return CustomDLL{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "packages", customDLLPackage, id, customDLLName))
	if err != nil {
		return CustomDLL{}, nil, err
	}
	if hashBytes(data) != dll.SHA256 {
		return CustomDLL{}, nil, errors.New("custom DLL hash mismatch")
	}
	return dll, data, nil
}

func listCustomDLLs(root string) ([]CustomDLL, error) {
	entries, err := os.ReadDir(filepath.Join(root, "packages", customDLLPackage))
	if errors.Is(err, os.ErrNotExist) {
		return []CustomDLL{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]CustomDLL, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !isCustomDLLID(entry.Name()) {
			continue
		}
		dll, err := readCustomDLLInfo(root, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("custom DLL %s: %w", entry.Name(), err)
		}
		result = append(result, dll)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ImportedAt > result[j].ImportedAt })
	return result, nil
}
