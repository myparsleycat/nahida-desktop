package xxmi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
)

const legacyRuntimeURL = "https://raw.githubusercontent.com/SilentNightSound/GI-Model-Importer/refs/heads/main/3dmigoto%20GIMI%20(for%20playing%20mods).zip"

type LegacyRuntimeSource struct {
	URL       string            `json:"url"`
	ZipSHA256 string            `json:"zipSha256"`
	Files     map[string]string `json:"files"`
	FetchedAt string            `json:"fetchedAt"`
}

func (x *XXMI) UpdateLegacyRuntime(ctx context.Context) (string, error) {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()

	file, err := os.CreateTemp("", "nahida-legacy-*.zip")
	if err != nil {
		return "", err
	}
	zipPath := file.Name()
	_ = file.Close()
	defer func() { _ = os.Remove(zipPath) }()
	repo := github.Repo{Owner: "SilentNightSound", Name: "GI-Model-Importer"}
	if err := x.downloadPackageFile(
		ctx,
		"legacy-3dmigoto", "",
		github.FileRequest{Repo: repo, URL: legacyRuntimeURL, Destination: zipPath},
	); err != nil {
		return "", fmt.Errorf("download legacy runtime: %w", err)
	}
	return importLegacyRuntimeZip(ctx, zipPath, legacyRuntimeURL)
}

func (x *XXMI) ImportLegacyRuntimeZip(ctx context.Context, zipPath string) (string, error) {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()

	return importLegacyRuntimeZip(ctx, zipPath, "")
}

func importLegacyRuntimeZip(ctx context.Context, zipPath, sourceURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	zipFile, err := os.Open(zipPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = zipFile.Close() }()
	stat, err := zipFile.Stat()
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 512<<20 {
		return "", errors.New("legacy runtime zip is not a regular file or is too large")
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, zipFile); err != nil {
		return "", err
	}
	zipHash := hex.EncodeToString(hasher.Sum(nil))
	root, err := xxmiCacheRoot()
	if err != nil {
		return "", err
	}
	return extractLegacyRuntime(ctx, zipFile, stat.Size(), zipHash, sourceURL, root)
}

func extractLegacyRuntime(
	ctx context.Context,
	zipFile *os.File,
	zipSize int64,
	zipHash, sourceURL, root string,
) (string, error) {
	id := zipHash[:12]
	parent := filepath.Join(root, "packages", "legacy-3dmigoto")
	destination := filepath.Join(parent, id)
	if _, err := os.Stat(filepath.Join(destination, "source.json")); err == nil {
		if err := verifyLegacyRuntimeCache(destination, zipHash); err == nil {
			return id, nil
		}
		return "", fmt.Errorf("cached legacy runtime %s is corrupted", id)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	reader, err := zip.NewReader(zipFile, zipSize)
	if err != nil {
		return "", fmt.Errorf("open legacy runtime zip: %w", err)
	}
	loaderFolder := ""
	for _, entry := range reader.File {
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if path.Base(name) == "3DMigoto Loader.exe" && len(strings.Split(name, "/")) <= 4 {
			loaderFolder = path.Dir(name)
			break
		}
	}
	if loaderFolder == "" {
		return "", errors.New("legacy runtime zip is missing 3DMigoto Loader.exe within depth 3")
	}
	staging, err := os.MkdirTemp(parent, id+".tmp-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	source := LegacyRuntimeSource{
		URL:       sourceURL,
		ZipSHA256: zipHash,
		Files:     map[string]string{},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		base := path.Base(name)
		if path.Dir(name) != loaderFolder ||
			(base != "3DMigoto Loader.exe" && !strings.EqualFold(path.Ext(base), ".dll")) {
			continue
		}
		if base == "." || base == ".." || entry.UncompressedSize64 > 64<<20 || !entry.Mode().IsRegular() {
			return "", fmt.Errorf("invalid legacy runtime entry %q", entry.Name)
		}
		input, err := entry.Open()
		if err != nil {
			return "", err
		}
		output, err := os.OpenFile(filepath.Join(staging, base), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return "", err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, 64<<20+1))
		closeErr := output.Close()
		_ = input.Close()
		if copyErr != nil || closeErr != nil {
			return "", errors.Join(copyErr, closeErr)
		}
		if written > 64<<20 {
			return "", fmt.Errorf("legacy runtime entry %q exceeds size limit", entry.Name)
		}
		source.Files[base] = hex.EncodeToString(hash.Sum(nil))
	}
	if source.Files["3DMigoto Loader.exe"] == "" || source.Files["d3d11.dll"] == "" {
		return "", errors.New("legacy runtime zip is missing loader or d3d11.dll")
	}
	data, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, "source.json"), data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(staging, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			return id, nil
		}
		return "", err
	}
	return id, nil
}

func verifyLegacyRuntimeCache(folder, zipHash string) error {
	data, err := os.ReadFile(filepath.Join(folder, "source.json"))
	if err != nil {
		return err
	}
	var source LegacyRuntimeSource
	if err := json.Unmarshal(data, &source); err != nil {
		return err
	}
	if source.ZipSHA256 != zipHash || source.Files["3DMigoto Loader.exe"] == "" || source.Files["d3d11.dll"] == "" {
		return errors.New("legacy runtime metadata mismatch")
	}
	for name, want := range source.Files {
		if filepath.Base(name) != name {
			return fmt.Errorf("invalid cached legacy runtime file %q", name)
		}
		file, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		_ = file.Close()
		if copyErr != nil {
			return copyErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != want {
			return fmt.Errorf("legacy runtime file %s hash mismatch", name)
		}
	}
	return nil
}
