package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	textureProcessTimeout = 10 * time.Minute
)

type TextureResizeRunInput struct {
	TargetPath string                `json:"targetPath"`
	Settings   TextureResizeSettings `json:"settings"`
}

type TextureResizeModInput struct {
	Settings TextureResizeSettings `json:"settings"`
}

type TextureResizeFileRunInput struct {
	FilePath string                `json:"filePath"`
	Settings TextureResizeSettings `json:"settings"`
}

type TextureResizeFileResult struct {
	FilePath       string  `json:"filePath"`
	Status         string  `json:"status"`
	OriginalWidth  int     `json:"originalWidth"`
	OriginalHeight int     `json:"originalHeight"`
	OutputWidth    int     `json:"outputWidth"`
	OutputHeight   int     `json:"outputHeight"`
	OriginalFormat string  `json:"originalFormat"`
	OutputFormat   string  `json:"outputFormat"`
	BackupCreated  bool    `json:"backupCreated"`
	Message        *string `json:"message,omitempty"`
}

type TextureResizeResult struct {
	TargetPath string                    `json:"targetPath"`
	Processed  int                       `json:"processed"`
	Updated    int                       `json:"updated"`
	Skipped    int                       `json:"skipped"`
	Failed     int                       `json:"failed"`
	Files      []TextureResizeFileResult `json:"files"`
}

type TextureResizeProgressEvent struct {
	Status         string  `json:"status"`
	Operation      *string `json:"operation,omitempty"`
	FilePath       *string `json:"filePath,omitempty"`
	FileName       *string `json:"fileName,omitempty"`
	TotalFiles     *int    `json:"totalFiles,omitempty"`
	ProcessedFiles *int    `json:"processedFiles,omitempty"`
	Error          *string `json:"error,omitempty"`
}

type TextureUpscaleProgressEvent struct {
	Phase    string   `json:"phase"`
	Percent  *float64 `json:"percent"`
	Message  *string  `json:"message,omitempty"`
	FilePath *string  `json:"filePath,omitempty"`
}

type textureDecodedMetadata struct {
	Width, Height, Layers, Mipmaps int
	Format                         string
}

type textureEncodedMetadata struct {
	Path          string `json:"path"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	OutputFormat  string `json:"outputFormat"`
	BackupCreated bool   `json:"backupCreated"`
}

func (t *Tools) GetTextureResizeState() TextureResizeProgressEvent {
	t.textureMu.Lock()
	defer t.textureMu.Unlock()
	return t.textureState
}

func (t *Tools) ResizeTextureFolder(ctx context.Context, input TextureResizeRunInput) (TextureResizeResult, error) {
	target := strings.TrimSpace(input.TargetPath)
	if target == "" {
		return TextureResizeResult{}, contractError("Target path is required.")
	}
	settings, err := t.saveFullTextureResizeSettings(ctx, input.Settings)
	if err != nil {
		return TextureResizeResult{}, err
	}
	if isTextureUpscaleOperation(settings.Operation) {
		return TextureResizeResult{}, contractError("Folder upscale is not supported.")
	}
	resolved, err := filepath.Abs(target)
	if err != nil {
		return TextureResizeResult{}, err
	}
	return t.runTextureResizeJob(ctx, resolved, settings, false, func(jobCtx context.Context) (TextureResizeResult, error) {
		return t.runTextureResize(jobCtx, resolved, settings)
	})
}

func (t *Tools) ResizeTextureMod(ctx context.Context, modPath string, input TextureResizeModInput) (TextureResizeResult, error) {
	return t.ResizeTextureFolder(ctx, TextureResizeRunInput{TargetPath: modPath, Settings: input.Settings})
}

func (t *Tools) ResizeTextureFile(ctx context.Context, input TextureResizeFileRunInput) (TextureResizeResult, error) {
	filePath := strings.TrimSpace(input.FilePath)
	if filePath == "" {
		return TextureResizeResult{}, contractError("File path is required.")
	}
	settings, err := t.saveFullTextureResizeSettings(ctx, input.Settings)
	if err != nil {
		return TextureResizeResult{}, err
	}
	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return TextureResizeResult{}, err
	}
	return t.runTextureResizeJob(ctx, resolved, settings, true, func(jobCtx context.Context) (TextureResizeResult, error) {
		if isTextureUpscaleOperation(settings.Operation) {
			return t.upscaleTextureFile(jobCtx, resolved, settings)
		}
		return t.runTextureResize(jobCtx, resolved, settings)
	})
}

func (t *Tools) saveFullTextureResizeSettings(ctx context.Context, settings TextureResizeSettings) (TextureResizeSettings, error) {
	return t.SaveTextureResizeSettings(ctx, TextureResizeSettingsPatch{
		Mode: &settings.Mode, Operation: &settings.Operation, Percent: &settings.Percent,
		CustomWidth: &settings.CustomWidth, CustomHeight: &settings.CustomHeight,
		OutputFormat: &settings.OutputFormat, Backup: &settings.Backup,
		UpscaleScale: &settings.UpscaleScale, UpscaleModel: &settings.UpscaleModel,
	})
}

func (t *Tools) runTextureResizeJob(ctx context.Context, path string, settings TextureResizeSettings, singleFile bool, work func(context.Context) (TextureResizeResult, error)) (TextureResizeResult, error) {
	running := TextureResizeProgressEvent{
		Status: "running", Operation: stringPointer(settings.Operation), FilePath: stringPointer(path),
		FileName: stringPointer(filepath.Base(path)),
	}
	if singleFile {
		running.TotalFiles, running.ProcessedFiles = intPointer(1), intPointer(0)
	}
	jobID := t.beginTextureJob(running)
	result, err := work(ctx)
	if err != nil {
		failed := running
		failed.Status, failed.Error = "failed", stringPointer(err.Error())
		t.settleTextureJob(jobID, failed)
		return TextureResizeResult{}, infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: "texture-resize", Stage: "execute",
			Fields: map[string]any{"path": path, "mode": settings.Operation, "singleFile": singleFile},
		})
	}
	completed := running
	completed.Status = "completed"
	completed.TotalFiles, completed.ProcessedFiles = intPointer(result.Processed), intPointer(result.Processed)
	t.settleTextureJob(jobID, completed)
	return result, nil
}

func (t *Tools) beginTextureJob(running TextureResizeProgressEvent) uint64 {
	t.textureEventMu.Lock()
	defer t.textureEventMu.Unlock()
	t.textureMu.Lock()
	t.textureNextJob++
	jobID := t.textureNextJob
	t.textureJobs[jobID] = running
	t.textureState = running
	t.textureMu.Unlock()
	t.emitEvent("tools:textureResizeProgress", running)
	return jobID
}

func (t *Tools) settleTextureJob(jobID uint64, terminal TextureResizeProgressEvent) {
	t.textureEventMu.Lock()
	defer t.textureEventMu.Unlock()
	t.textureMu.Lock()
	delete(t.textureJobs, jobID)
	var remaining *TextureResizeProgressEvent
	var highest uint64
	for id, state := range t.textureJobs {
		if remaining == nil || id > highest {
			copy := state
			remaining, highest = &copy, id
		}
	}
	toEmit := terminal
	if remaining != nil {
		toEmit = *remaining
	}
	t.textureState = toEmit
	idleAfterEmit := len(t.textureJobs) == 0
	t.textureMu.Unlock()
	t.emitEvent("tools:textureResizeProgress", toEmit)
	if idleAfterEmit {
		t.textureMu.Lock()
		t.textureState = TextureResizeProgressEvent{Status: "idle"}
		t.textureMu.Unlock()
	}
}

func (t *Tools) runTextureResize(ctx context.Context, target string, settings TextureResizeSettings) (TextureResizeResult, error) {
	return executeTextureResize(ctx, textureResizeRequest{
		TargetPath: target, Mode: settings.Mode, Operation: settings.Operation,
		Percent: settings.Percent, CustomWidth: settings.CustomWidth, CustomHeight: settings.CustomHeight,
		OutputFormat: settings.OutputFormat, Backup: settings.Backup,
	})
}

func (t *Tools) upscaleTextureFile(ctx context.Context, path string, settings TextureResizeSettings) (TextureResizeResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return TextureResizeResult{}, err
	}
	metadata, err := parseDDS(raw)
	if err != nil {
		return TextureResizeResult{}, err
	}
	if reason := textureUpscaleSkipReason(metadata, settings.UpscaleScale); reason != nil {
		t.emitTextureUpscaleProgress("done", floatPointer(100), reason, path)
		return skippedTextureResizeResult(path, metadata, *reason), nil
	}
	engine := textureUpscaleEngine(settings.UpscaleModel)
	displayName := map[string]string{"realesrgan": "Real-ESRGAN", "realcugan": "Real-CUGAN"}[engine]
	t.emitTextureUpscaleProgress("download", floatPointer(0), stringPointer("Preparing "+displayName+" runtime"), path)
	runtimeStatus, err := t.installTextureUpscaleRuntime(ctx, engine, func(phase string, percent *float64) {
		verb := "Downloading "
		if phase == "extract" {
			verb = "Extracting "
		}
		t.emitTextureUpscaleProgress(phase, percent, stringPointer(verb+displayName+" runtime"), path)
	})
	if err != nil {
		t.emitTextureUpscaleProgress("error", nil, stringPointer(err.Error()), path)
		return TextureResizeResult{}, err
	}
	if runtimeStatus.BinaryPath == nil || runtimeStatus.ModelsPath == nil {
		return TextureResizeResult{}, fmt.Errorf("%s runtime is not installed", displayName)
	}
	workDir, err := os.MkdirTemp("", "nhd-texture-upscale-")
	if err != nil {
		return TextureResizeResult{}, err
	}
	defer func() { t.reportCleanup(os.RemoveAll(workDir), "upscaleTextureFile") }()
	inputPNG, outputPNG := filepath.Join(workDir, "input.png"), filepath.Join(workDir, "output.png")
	t.emitTextureUpscaleProgress("decode", nil, stringPointer("Decoding DDS texture"), path)
	decoded, err := decodeDDSToPng(path, inputPNG)
	if err != nil {
		t.emitTextureUpscaleProgress("error", nil, stringPointer(err.Error()), path)
		return TextureResizeResult{}, err
	}
	if decoded.Layers > 1 {
		message := "Cubemap and layered DDS textures cannot be upscaled."
		t.emitTextureUpscaleProgress("done", floatPointer(100), stringPointer(message), path)
		return skippedTextureResizeResult(path, metadata, message), nil
	}
	outputFormat := resolveTextureUpscaleOutputFormat(settings, decoded.Format, metadata.colorSpace)
	if outputFormat == "" {
		message := "This DDS format cannot be re-encoded after upscaling."
		t.emitTextureUpscaleProgress("done", floatPointer(100), stringPointer(message), path)
		return skippedTextureResizeResult(path, metadata, message), nil
	}
	t.emitTextureUpscaleProgress("upscale", nil, stringPointer("Running "+displayName), path)
	if err := t.runNCNNUpscaler(ctx, engine, *runtimeStatus.BinaryPath, *runtimeStatus.ModelsPath, inputPNG, outputPNG, settings); err != nil {
		t.emitTextureUpscaleProgress("error", nil, stringPointer(err.Error()), path)
		return TextureResizeResult{}, err
	}
	t.emitTextureUpscaleProgress("encode", nil, stringPointer("Encoding DDS texture"), path)
	encoded, err := encodePNGToDDS(outputPNG, path, outputFormat, settings.Backup, decoded.Mipmaps > 1)
	if err != nil {
		t.emitTextureUpscaleProgress("error", nil, stringPointer(err.Error()), path)
		return TextureResizeResult{}, err
	}
	if expectedWidth, expectedHeight := decoded.Width*settings.UpscaleScale, decoded.Height*settings.UpscaleScale; encoded.Width != expectedWidth || encoded.Height != expectedHeight {
		if t.log != nil {
			t.log.Warn(
				fmt.Sprintf("%s output size %dx%d did not match expected %dx%d", displayName, encoded.Width, encoded.Height, expectedWidth, expectedHeight),
				"TextureResizer:upscale",
			)
		}
	}
	t.emitTextureUpscaleProgress("done", floatPointer(100), stringPointer("Texture upscale completed"), path)
	return TextureResizeResult{
		TargetPath: path, Processed: 1, Updated: 1, Files: []TextureResizeFileResult{{
			FilePath: path, Status: "updated", OriginalWidth: decoded.Width, OriginalHeight: decoded.Height,
			OutputWidth: encoded.Width, OutputHeight: encoded.Height, OriginalFormat: decoded.Format,
			OutputFormat: encoded.OutputFormat, BackupCreated: encoded.BackupCreated,
		}},
	}, nil
}

func (t *Tools) runNCNNUpscaler(parent context.Context, engine, binaryPath, modelsPath, inputPath, outputPath string, settings TextureResizeSettings) error {
	ctx, cancel := context.WithTimeout(parent, textureProcessTimeout)
	defer cancel()
	args := buildNCNNUpscalerArgs(engine, modelsPath, inputPath, outputPath, settings)
	command := exec.CommandContext(ctx, binaryPath, args...)
	command.Dir = filepath.Dir(binaryPath)
	configureTextureCommand(command)
	var stdout, stderr limitedTextureBuffer
	stdout.limit, stderr.limit = 1<<20, 1<<20
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		displayName := textureUpscaleDisplayName(engine)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New(formatTextureProcessTimeout(displayName, strings.TrimSpace(stderr.String())))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return errors.New(formatTextureProcessExit(displayName, exitErr.ExitCode(), strings.TrimSpace(stderr.String())))
		}
		return err
	}
	if message := strings.TrimSpace(stdout.String()); message != "" && t.log != nil {
		t.log.Info(message, "TextureResizer:upscale")
	}
	if !regularFile(outputPath) {
		return fmt.Errorf("%s did not create an output image", engine)
	}
	return nil
}

func textureUpscaleDisplayName(engine string) string {
	if engine == "realcugan" {
		return "Real-CUGAN"
	}
	return "Real-ESRGAN"
}

func formatTextureProcessTimeout(displayName, stderr string) string {
	return fmt.Sprintf("%s timed out after %dms%s", displayName, textureProcessTimeout.Milliseconds(), textureProcessErrorSuffix(stderr))
}

func formatTextureProcessExit(displayName string, code int, stderr string) string {
	return fmt.Sprintf("%s exited with code %d%s", displayName, code, textureProcessErrorSuffix(stderr))
}

func textureProcessErrorSuffix(stderr string) string {
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		return ": " + stderr
	}
	return ""
}

func buildNCNNUpscalerArgs(engine, modelsPath, inputPath, outputPath string, settings TextureResizeSettings) []string {
	if engine == "realcugan" {
		modelDir := "models-" + strings.TrimPrefix(settings.UpscaleModel, "realcugan-")
		return []string{"-i", inputPath, "-o", outputPath, "-n", "0", "-s", strconv.Itoa(settings.UpscaleScale), "-t", "0", "-c", "3", "-f", "png", "-m", filepath.Join(modelsPath, modelDir)}
	}
	return []string{"-i", inputPath, "-o", outputPath, "-n", settings.UpscaleModel, "-s", strconv.Itoa(settings.UpscaleScale), "-t", "0", "-f", "png", "-m", modelsPath}
}

func (t *Tools) emitTextureUpscaleProgress(phase string, percent *float64, message *string, path string) {
	event := TextureUpscaleProgressEvent{Phase: phase, Percent: percent, Message: message, FilePath: stringPointer(path)}
	t.emitEvent("tools:textureUpscaleProgress", event)
}

func textureUpscaleEngine(model string) string {
	if strings.HasPrefix(model, "realcugan-") {
		return "realcugan"
	}
	return "realesrgan"
}

func isTextureUpscaleOperation(operation string) bool {
	return operation == "upscale" || operation == "upscale_and_convert"
}

func resolveTextureUpscaleOutputFormat(settings TextureResizeSettings, decodedFormat, colorSpace string) string {
	available := availableFormats(colorSpace)
	if settings.Operation == "upscale_and_convert" && contains(available, settings.OutputFormat) {
		return settings.OutputFormat
	}
	if contains(available, decodedFormat) {
		return decodedFormat
	}
	return ""
}

func skippedTextureResizeResult(path string, metadata ddsMetadata, message string) TextureResizeResult {
	return TextureResizeResult{
		TargetPath: path, Processed: 1, Skipped: 1,
		Files: []TextureResizeFileResult{{
			FilePath: path, Status: "skipped", OriginalWidth: metadata.width, OriginalHeight: metadata.height,
			OutputWidth: metadata.width, OutputHeight: metadata.height, OriginalFormat: metadata.format,
			OutputFormat: metadata.format, Message: stringPointer(message),
		}},
	}
}

type limitedTextureBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedTextureBuffer) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > b.limit {
		remaining := max(0, b.limit-b.buffer.Len())
		if remaining > 0 {
			_, _ = b.buffer.Write(data[:remaining])
		}
		return remaining, errors.New("texture helper output exceeded limit")
	}
	return b.buffer.Write(data)
}

func (b *limitedTextureBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedTextureBuffer) String() string { return b.buffer.String() }

var _ io.Writer = (*limitedTextureBuffer)(nil)

func intPointer(value int) *int { return &value }
