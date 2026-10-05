package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

func (x *XXMI) installBuiltinImporterPackage(
	ctx context.Context, spec importerPackageSpec, cfg ImporterConfig, input InstallImporterPackageInput,
) (returnErr error) {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-install-importer-package")
	stage := "validate"
	rollbackState := "not-started"
	defer func() {
		if returnErr == nil {
			return
		}
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.installBuiltinImporterPackage", infra.Diagnostic{
			Operation: "install-importer-package", Stage: stage,
			Fields: map[string]any{
				"importer": spec.key, "version": input.Version, "importer_path": cfg.ImporterFolder,
				"rollback": rollbackState,
			},
		})
	}()
	if x.archive == nil || !x.github.Configured() {
		return errors.New("XXMI install services are not configured")
	}
	if err := ValidateImporterSettings(spec.key, cfg); err != nil {
		return err
	}
	if cfg.ImporterFolder == "" {
		return errors.New("XXMI importer folder is not configured")
	}
	if err := validateImporterFolderTarget(cfg.ImporterFolder); err != nil {
		return err
	}
	version := normalizeVersion(input.Version)
	if version == "" {
		return errors.New("invalid importer package version")
	}
	stage = "resolve-release"
	releases, err := x.github.AllReleases(ctx, spec.repo)
	if err != nil {
		return err
	}
	var release *github.Release
	for i := range releases {
		if !releases[i].Draft && normalizeVersion(releases[i].TagName) == version {
			release = &releases[i]
			break
		}
	}
	if release == nil {
		return fmt.Errorf("importer %s release %s not found", spec.key, version)
	}
	assetName := fmt.Sprintf(spec.assetFormat, version)
	assetURL := releaseAssetURL(*release, assetName)
	if assetURL == "" {
		return fmt.Errorf("importer %s asset %s not found", spec.key, assetName)
	}
	signature := releaseSignature(release.Body)
	if signature == "" && !input.AllowUnsigned {
		return errors.New("XXMI_UNSIGNED_RELEASE")
	}
	stage = "download"
	workDir, err := os.MkdirTemp("", "nahida-xxmi-importer-")
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(os.RemoveAll(workDir), "InstallImporterPackage") }()
	zipPath := filepath.Join(workDir, "package.zip")
	if err := x.downloadPackageFile(
		ctx,
		"importer:"+spec.key, version,
		github.FileRequest{Repo: spec.repo, URL: assetURL, Destination: zipPath},
	); err != nil {
		return err
	}
	stage = "verify"
	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	if !zipInfo.Mode().IsRegular() || zipInfo.Size() > 1<<30 {
		return errors.New("importer package is not a regular file or exceeds size limit")
	}
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	verification := PackageVerification{Version: version, Method: "ecdsa"}
	if signature != "" {
		if err := verifyPackageSignature(spec.publicKey, signature, zipBytes); err != nil {
			return err
		}
	} else {
		verifiedDigest, err := verifyReleaseDigest(*release, assetName, zipBytes)
		if err != nil {
			return err
		}
		verification.Method = "none"
		if verifiedDigest {
			verification.Method = "digest"
		}
	}
	stage = "extract"
	extractedPath, err := x.archive.Extract(
		ctx,
		zipPath,
		filepath.Join(workDir, "extracted"),
		infra.ExtractOptions{},
		nil,
	)
	if err != nil {
		return err
	}
	stagingDir := filepath.Join(workDir, "staging")
	if err := copyTreeContext(ctx, extractedPath, stagingDir); err != nil {
		return err
	}
	stagedVersion := readImporterVersion(stagingDir, spec)
	if stagedVersion == nil || normalizeVersion(*stagedVersion) != version {
		return fmt.Errorf("importer package version mismatch: expected %s", version)
	}
	stage = "prepare-transaction"
	if err := os.MkdirAll(filepath.Dir(cfg.ImporterFolder), 0o700); err != nil {
		return err
	}
	transaction, err := beginImporterInstallTransaction(ctx, cfg.ImporterFolder, "", spec.key)
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(transaction.Close(), "InstallImporterPackage") }()
	prepared := false
	committed := false
	defer func() {
		if !prepared || committed {
			return
		}
		rollbackState = "rolling-back"
		if err := transaction.rollback(); err != nil {
			rollbackState = "rollback-failed"
			returnErr = errors.Join(returnErr, err)
			return
		}
		rollbackState = "rolled-back"
	}()
	stageRoot, err := transaction.prepare(ctx)
	if err != nil {
		prepared = transaction.state != ""
		return err
	}
	prepared = true
	var iniBackup []byte
	iniBackup, _, err = stageRoot.readFile("d3dx.ini")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = stageRoot.Close()
		return err
	}
	if iniBackup != nil {
		root, err := xxmiCacheRoot()
		if err != nil {
			_ = stageRoot.Close()
			return err
		}
		backupRoot := filepath.Join(root, "backups")
		if err := os.MkdirAll(backupRoot, 0o700); err != nil {
			_ = stageRoot.Close()
			return err
		}
		backupFolder, err := os.MkdirTemp(backupRoot, spec.key+" "+time.Now().Format("2006-01-02 15-04-05")+"-")
		if err != nil {
			_ = stageRoot.Close()
			return err
		}
		if err := os.WriteFile(filepath.Join(backupFolder, "d3dx.ini"), iniBackup, 0o600); err != nil {
			_ = stageRoot.Close()
			return err
		}
	}
	stage = "pre-install"
	if _, err := executeXcmdDeletesRoot(ctx, stagingDir, stageRoot, "PreInstall"); err != nil {
		_ = stageRoot.Close()
		return err
	}
	stage = "copy-package"
	if err := copyTreeFilterToRoot(ctx, stagingDir, stageRoot, shouldSkipImporterMods); err != nil {
		_ = stageRoot.Close()
		return err
	}
	stage = "post-install"
	if _, err := executeXcmdDeletesFromRoot(ctx, stageRoot, stageRoot, "PostInstall"); err != nil {
		_ = stageRoot.Close()
		return err
	}
	if !cfg.OverwriteINI && iniBackup != nil {
		if err := stageRoot.writeFileAtomic(ctx, "d3dx.ini", bytes.NewReader(iniBackup), 0o600, nil); err != nil {
			_ = stageRoot.Close()
			return err
		}
	}
	verificationData, err := json.Marshal(verification)
	if err != nil {
		_ = stageRoot.Close()
		return err
	}
	if err := stageRoot.writeFileAtomic(
		ctx,
		packageVerificationName,
		bytes.NewReader(verificationData),
		0o600,
		nil,
	); err != nil {
		_ = stageRoot.Close()
		return err
	}
	if _, err := stageRoot.root.Stat("Mods"); errors.Is(err, os.ErrNotExist) {
		if err := stageRoot.root.Mkdir("Mods", 0o755); err != nil {
			_ = stageRoot.Close()
			return err
		}
	} else if err != nil {
		_ = stageRoot.Close()
		return err
	}
	if err := stageRoot.Close(); err != nil {
		return err
	}
	stage = "commit"
	if _, _, err := transaction.commit(ctx, nil); err != nil {
		return err
	}
	if input.Config != nil {
		stage = "save-config"
		if err := x.SaveImporterConfig(ctx, spec.key, cfg); err != nil {
			return err
		}
	}
	committed = true
	rollbackState = "committed"
	x.reportCleanup(transaction.finish(), "InstallImporterPackage")
	return nil
}

func verifyReleaseDigest(release github.Release, assetName string, data []byte) (bool, error) {
	for _, asset := range release.Assets {
		if asset.Name != assetName || asset.Digest == "" {
			continue
		}
		want, ok := strings.CutPrefix(asset.Digest, "sha256:")
		if !ok {
			return false, errors.New("unsupported release asset digest")
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(want, hex.EncodeToString(digest[:])) {
			return false, errors.New("release asset digest mismatch")
		}
		return true, nil
	}
	return false, nil
}
