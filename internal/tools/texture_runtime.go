package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

const (
	textureRuntimeDownloadStallTimeout = 30 * time.Second
	textureRuntimeDownloadTimeout      = 10 * time.Minute
)

type TextureUpscaleRuntimeStatus struct {
	Installed    bool    `json:"installed"`
	Version      *string `json:"version"`
	BinaryPath   *string `json:"binaryPath"`
	ModelsPath   *string `json:"modelsPath"`
	NeedsInstall bool    `json:"needsInstall"`
}

type TextureUpscaleRuntimeStatuses struct {
	Realesrgan TextureUpscaleRuntimeStatus `json:"realesrgan"`
	Realcugan  TextureUpscaleRuntimeStatus `json:"realcugan"`
}

type textureRuntimeSpec struct {
	dirName, binaryName, version, settingPrefix string
	displayName, downloadURL, archiveSHA256     string
	modelsRelative                              string
	modelDirNames                               []string
	requiredModels                              []string
}

var realesrganSpec = textureRuntimeSpec{
	dirName: "realesrgan-ncnn-vulkan", binaryName: "realesrgan-ncnn-vulkan.exe", version: "20220424",
	settingPrefix: "mod_tools:realesrgan-ncnn-vulkan", modelsRelative: "models",
	displayName:    "Real-ESRGAN",
	downloadURL:    "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip",
	archiveSHA256:  "abc02804e17982a3be33675e4d471e91ea374e65b70167abc09e31acb412802d",
	modelDirNames:  []string{"models"},
	requiredModels: []string{"realesr-animevideov3-x2", "realesr-animevideov3-x3", "realesr-animevideov3-x4", "realesrgan-x4plus-anime", "realesrgan-x4plus"},
}

var realcuganSpec = textureRuntimeSpec{
	dirName: "realcugan-ncnn-vulkan", binaryName: "realcugan-ncnn-vulkan.exe", version: "20220728",
	settingPrefix:  "mod_tools:realcugan-ncnn-vulkan",
	displayName:    "Real-CUGAN",
	downloadURL:    "https://github.com/nihui/realcugan-ncnn-vulkan/releases/download/20220728/realcugan-ncnn-vulkan-20220728-windows.zip",
	archiveSHA256:  "c6e08d46c11704b1e3a1ada9ddd591cb5005f52f132136c8633ba25def400e01",
	modelDirNames:  []string{"models-pro", "models-se", "models-nose"},
	requiredModels: []string{"models-pro/up2x-no-denoise", "models-pro/up3x-no-denoise", "models-se/up2x-no-denoise", "models-se/up3x-no-denoise", "models-se/up4x-no-denoise", "models-nose/up2x-no-denoise"},
}

func (t *Tools) GetTextureUpscaleRuntimeStatus(ctx context.Context) (TextureUpscaleRuntimeStatuses, error) {
	realesrgan, err := t.textureRuntimeStatus(ctx, realesrganSpec)
	if err != nil {
		return TextureUpscaleRuntimeStatuses{}, err
	}
	realcugan, err := t.textureRuntimeStatus(ctx, realcuganSpec)
	if err != nil {
		return TextureUpscaleRuntimeStatuses{}, err
	}
	return TextureUpscaleRuntimeStatuses{Realesrgan: realesrgan, Realcugan: realcugan}, nil
}

func (t *Tools) textureRuntimeStatus(ctx context.Context, spec textureRuntimeSpec) (TextureUpscaleRuntimeStatus, error) {
	client, err := t.requireClient()
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	root, err := t.appDataPath(filepath.Join(appdata.ToolsDir, spec.dirName))
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	binaryPath := filepath.Join(root, spec.binaryName)
	modelsPath := root
	if spec.modelsRelative != "" {
		modelsPath = filepath.Join(root, spec.modelsRelative)
	}
	if stored, err := client.Settings.GetValue(ctx, spec.settingPrefix+":binary-path"); err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	} else if stored != nil && pathExists(*stored) {
		binaryPath = *stored
	}
	if stored, err := client.Settings.GetValue(ctx, spec.settingPrefix+":models-path"); err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	} else if stored != nil && pathExists(*stored) {
		modelsPath = *stored
	}
	installed := isTextureRuntimeInstalled(binaryPath, modelsPath, spec.requiredModels)
	if !installed {
		return TextureUpscaleRuntimeStatus{NeedsInstall: true}, nil
	}
	version := spec.version
	if stored, err := client.Settings.GetValue(ctx, spec.settingPrefix+":installed-version"); err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	} else if stored != nil && *stored != "" {
		version = *stored
	}
	return TextureUpscaleRuntimeStatus{Installed: true, Version: &version, BinaryPath: &binaryPath, ModelsPath: &modelsPath}, nil
}

// installTextureUpscaleRuntime downloads and atomically promotes one pinned
// ncnn-vulkan runtime. It is intentionally internal: Electron installed it on
// demand from the file-upscale operation rather than exposing a separate IPC.
func (t *Tools) installTextureUpscaleRuntime(ctx context.Context, engine string, progress func(string, *float64)) (TextureUpscaleRuntimeStatus, error) {
	spec, err := textureRuntimeSpecForEngine(engine)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	return t.installTextureRuntime(ctx, spec, progress)
}

func (t *Tools) installTextureRuntime(ctx context.Context, spec textureRuntimeSpec, progress func(string, *float64)) (TextureUpscaleRuntimeStatus, error) {
	t.textureRuntimeMu.Lock()
	defer t.textureRuntimeMu.Unlock()
	if status, statusErr := t.textureRuntimeStatus(ctx, spec); statusErr != nil || status.Installed {
		return status, statusErr
	}
	if t.download == nil || t.archive == nil {
		return TextureUpscaleRuntimeStatus{}, errors.New("texture runtime installer is unavailable")
	}
	if t.appData == nil {
		return TextureUpscaleRuntimeStatus{}, errors.New("tools service has no app data store")
	}
	toolsRoot, err := t.appData.EnsureDir(appdata.ToolsDir)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, fmt.Errorf("create texture tools directory: %w", err)
	}
	installRoot, err := os.MkdirTemp(toolsRoot, "."+spec.dirName+"-install-")
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, fmt.Errorf("create texture runtime staging directory: %w", err)
	}
	defer func() { t.reportTextureCleanup(os.RemoveAll(installRoot), installRoot) }()
	archivePath := filepath.Join(installRoot, spec.dirName+".zip")
	err = t.downloadTextureRuntimeArchive(
		ctx,
		spec,
		archivePath,
		progress,
		textureRuntimeDownloadTimeout,
		textureRuntimeDownloadStallTimeout,
	)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, fmt.Errorf("failed to download %s runtime: %w", spec.displayName, err)
	}
	if err := verifyTextureRuntimeArchive(archivePath, spec); err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	emitTextureRuntimeProgress(progress, "extract", nil)
	extractedRoot, err := t.archive.Extract(ctx, archivePath, filepath.Join(installRoot, "extract"), infra.ExtractOptions{}, nil)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, fmt.Errorf("extract %s runtime: %w", spec.displayName, err)
	}
	layoutRoot, err := resolveTextureRuntimeLayout(extractedRoot, spec)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	stageRoot := filepath.Join(toolsRoot, "."+spec.dirName+"-ready-"+filepath.Base(installRoot))
	if err := copyTextureRuntimeLayout(layoutRoot, stageRoot, spec); err != nil {
		t.reportTextureCleanup(os.RemoveAll(stageRoot), stageRoot)
		return TextureUpscaleRuntimeStatus{}, err
	}
	defer func() { t.reportTextureCleanup(os.RemoveAll(stageRoot), stageRoot) }()
	if err := promoteTextureRuntime(stageRoot, filepath.Join(toolsRoot, spec.dirName), t.reportTextureCleanup); err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	client, err := t.requireClient()
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	values := map[string]string{
		spec.settingPrefix + ":installed-version": spec.version,
		spec.settingPrefix + ":binary-path":       filepath.Join(toolsRoot, spec.dirName, spec.binaryName),
		spec.settingPrefix + ":models-path":       filepath.Join(toolsRoot, spec.dirName, filepath.FromSlash(spec.modelsRelative)),
	}
	if spec.modelsRelative == "" {
		values[spec.settingPrefix+":models-path"] = filepath.Join(toolsRoot, spec.dirName)
	}
	for key, value := range values {
		if err := client.Settings.Upsert(ctx, key, &value); err != nil {
			return TextureUpscaleRuntimeStatus{}, err
		}
	}
	status, err := t.textureRuntimeStatus(ctx, spec)
	if err != nil {
		return TextureUpscaleRuntimeStatus{}, err
	}
	if !status.Installed {
		return TextureUpscaleRuntimeStatus{}, fmt.Errorf("%s runtime installation is incomplete", spec.displayName)
	}
	return status, nil
}

func (t *Tools) downloadTextureRuntimeArchive(
	ctx context.Context,
	spec textureRuntimeSpec,
	archivePath string,
	progress func(string, *float64),
	timeout time.Duration,
	stallTimeout time.Duration,
) error {
	timeoutCtx, stopTimeout := context.WithTimeoutCause(ctx, timeout, errors.New("timed out"))
	defer stopTimeout()
	downloadCtx, cancelDownload := context.WithCancelCause(timeoutCtx)
	defer cancelDownload(nil)

	activity := make(chan struct{}, 1)
	done := make(chan struct{})
	go watchTextureRuntimeDownload(downloadCtx, cancelDownload, activity, done, stallTimeout)
	defer close(done)

	emitTextureRuntimeProgress(progress, "download", floatPointer(0))
	var received, contentLength int64
	err := t.download.File(downloadCtx, infra.DownloadRequest{
		URL: spec.downloadURL, Destination: archivePath,
		Header: http.Header{"User-Agent": []string{"Nahida Desktop"}},
		OnResponse: func(length int64) {
			received = 0
			contentLength = length
			notifyTextureRuntimeDownloadActivity(activity)
		},
		Progress: func(delta int64) {
			received += delta
			notifyTextureRuntimeDownloadActivity(activity)
			if contentLength > 0 {
				percent := min(100, float64(received)/float64(contentLength)*100)
				emitTextureRuntimeProgress(progress, "download", &percent)
				return
			}
			emitTextureRuntimeProgress(progress, "download", nil)
		},
	})
	if err != nil {
		if cause := context.Cause(downloadCtx); cause != nil {
			return cause
		}
		return err
	}
	emitTextureRuntimeProgress(progress, "download", floatPointer(100))
	return nil
}

func watchTextureRuntimeDownload(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	activity <-chan struct{},
	done <-chan struct{},
	stallTimeout time.Duration,
) {
	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-activity:
			if timer == nil {
				timer = time.NewTimer(stallTimeout)
				timerC = timer.C
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(stallTimeout)
		case <-timerC:
			cancel(errors.New("stalled"))
			return
		}
	}
}

func notifyTextureRuntimeDownloadActivity(activity chan<- struct{}) {
	select {
	case activity <- struct{}{}:
	default:
	}
}

func textureRuntimeSpecForEngine(engine string) (textureRuntimeSpec, error) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "realesrgan":
		return realesrganSpec, nil
	case "realcugan":
		return realcuganSpec, nil
	default:
		return textureRuntimeSpec{}, contractError(fmt.Sprintf("Unsupported texture upscale engine '%s'.", engine))
	}
}

func verifyTextureRuntimeArchive(path string, spec textureRuntimeSpec) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), spec.archiveSHA256) {
		return fmt.Errorf("downloaded %s archive checksum mismatch", spec.displayName)
	}
	return nil
}

func resolveTextureRuntimeLayout(root string, spec textureRuntimeSpec) (string, error) {
	var binaryPath string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), spec.binaryName) {
			binaryPath = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || binaryPath == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("extracted %s archive is missing the executable", spec.displayName)
	}
	layoutRoot := filepath.Dir(binaryPath)
	entries, err := os.ReadDir(layoutRoot)
	if err != nil {
		return "", err
	}
	for _, name := range spec.modelDirNames {
		found := false
		for _, entry := range entries {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && strings.EqualFold(entry.Name(), name) {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("extracted %s archive is missing the %s directory", spec.displayName, name)
		}
	}
	return layoutRoot, nil
}

func copyTextureRuntimeLayout(source, target string, spec textureRuntimeSpec) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	wantedDirs := make(map[string]bool, len(spec.modelDirNames))
	for _, name := range spec.modelDirNames {
		wantedDirs[strings.ToLower(name)] = true
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if entry.IsDir() {
			if wantedDirs[lower] {
				if err := copyTextureRuntimeTree(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
					return err
				}
			}
			continue
		}
		if strings.EqualFold(name, spec.binaryName) || strings.HasSuffix(lower, ".dll") {
			if err := copyRegularFile(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
				return err
			}
		}
	}
	for _, model := range spec.requiredModels {
		for _, extension := range []string{".param", ".bin"} {
			modelRoot := target
			if spec.modelsRelative != "" {
				modelRoot = filepath.Join(target, filepath.FromSlash(spec.modelsRelative))
			}
			if !regularFile(filepath.Join(modelRoot, filepath.FromSlash(model)+extension)) {
				return fmt.Errorf("staged %s runtime is missing model %s%s", spec.displayName, model, extension)
			}
		}
	}
	return nil
}

func copyTextureRuntimeTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyRegularFile(path, destination)
	})
}

func promoteTextureRuntime(stageRoot, targetRoot string, reports ...func(error, string)) error {
	parent := filepath.Dir(targetRoot)
	backupRoot := filepath.Join(parent, "."+filepath.Base(targetRoot)+"-previous")
	if err := os.RemoveAll(backupRoot); err != nil {
		for _, report := range reports {
			report(err, backupRoot)
		}
	}
	hadTarget := pathExists(targetRoot)
	if hadTarget {
		if err := os.Rename(targetRoot, backupRoot); err != nil {
			return fmt.Errorf("replace texture runtime: %w", err)
		}
	}
	if err := os.Rename(stageRoot, targetRoot); err != nil {
		var rollbackErr error
		if hadTarget {
			rollbackErr = os.Rename(backupRoot, targetRoot)
		}
		return infra.WithCause(fmt.Errorf("promote texture runtime: %w", err), infra.AnnotateError(rollbackErr, infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"path": targetRoot, "backupPath": backupRoot}}))
	}
	if hadTarget {
		if err := os.RemoveAll(backupRoot); err != nil {
			for _, report := range reports {
				report(err, backupRoot)
			}
		}
	}
	return nil
}

func emitTextureRuntimeProgress(callback func(string, *float64), phase string, percent *float64) {
	if callback != nil {
		callback(phase, percent)
	}
}

func floatPointer(value float64) *float64 { return &value }

func isTextureRuntimeInstalled(binaryPath, modelsPath string, models []string) bool {
	if !pathExists(binaryPath) || !pathExists(modelsPath) {
		return false
	}
	for _, model := range models {
		if !pathExists(filepath.Join(modelsPath, filepath.FromSlash(model)+".param")) ||
			!pathExists(filepath.Join(modelsPath, filepath.FromSlash(model)+".bin")) {
			return false
		}
	}
	return true
}

func isRealcuganRuntimeInstalled(binaryPath, runtimeRoot string) bool {
	return isTextureRuntimeInstalled(binaryPath, runtimeRoot, realcuganSpec.requiredModels)
}

func pathExists(path string) bool { _, err := os.Stat(path); return err == nil }
func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (t *Tools) reportTextureCleanup(err error, path string) {
	_ = infra.ReportError(t.log, err, "Tools", infra.Diagnostic{Operation: "install-texture-runtime", Stage: "cleanup", Fields: map[string]any{"path": path}})
}
