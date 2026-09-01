package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/db"
)

const fixToolLogEvent = "ftm:log"

type FixToolLogEvent struct {
	Message     string `json:"message"`
	ReplaceLast bool   `json:"replaceLast,omitempty"`
}

type CreateScriptPresetInput struct {
	Name      string   `json:"name"`
	ScriptIDs []string `json:"scriptIds"`
}

func (t *Tools) emitFixToolLog(message string, replaceLast bool) {
	t.emitEvent(fixToolLogEvent, FixToolLogEvent{Message: message, ReplaceLast: replaceLast})
}

func (t *Tools) GetScripts(ctx context.Context) ([]db.ScriptBasicRow, error) {
	client, err := t.requireClient()
	if err != nil {
		return nil, err
	}
	return client.Scripts.ListBasic(ctx)
}

func (t *Tools) SaveScript(ctx context.Context, inputPath string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	if strings.TrimSpace(inputPath) == "" {
		return contractError("Path is required")
	}
	info, err := os.Stat(inputPath)
	if errors.Is(err, os.ErrNotExist) {
		return contractError("File does not exist")
	}
	if err != nil {
		return fmt.Errorf("stat script: %w", err)
	}
	if !info.Mode().IsRegular() {
		return contractError("Path is not a regular file")
	}

	ext := strings.ToLower(filepath.Ext(inputPath))
	var scriptType db.ScriptType
	switch ext {
	case ".py":
		scriptType = db.ScriptTypePython
	case ".exe":
		scriptType = db.ScriptTypeExec
	default:
		return contractError("Invalid file type (only .py or .exe allowed)")
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read script: %w", err)
	}
	fileHash := sha256Hex(data)
	name := filepath.Base(inputPath)
	existing, err := client.Scripts.FindBySHA256OrName(ctx, fileHash, name)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.SHA256 == fileHash {
			return contractError("Already exists same file")
		}
		if existing.Name == name {
			return contractError("Already exists same name")
		}
	}

	compressed, err := compressZstd(data)
	if err != nil {
		return err
	}
	id, err := newToolsID()
	if err != nil {
		return err
	}
	zstdSize := int64(len(compressed))
	zstdHash := sha256Hex(compressed)
	return client.Scripts.Insert(ctx, db.ScriptRow{
		ID: id, Name: name, Source: compressed, IsSrcZstd: true, Type: scriptType,
		Size: int64(len(data)), ZstdSize: &zstdSize, SHA256: fileHash, ZstdSHA256: &zstdHash,
	})
}

func (t *Tools) DeleteScript(ctx context.Context, scriptID string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	script, err := client.Scripts.FindByID(ctx, scriptID)
	if err != nil {
		return err
	}
	if script == nil {
		return contractError("Script not found")
	}
	usage, err := client.ScriptPresetItems.FindUsageByScriptID(ctx, scriptID)
	if err != nil {
		return err
	}
	if usage != nil {
		return contractError(fmt.Sprintf("Script is used in a preset: %s", usage.PresetName))
	}
	return client.Scripts.Delete(ctx, scriptID)
}

func (t *Tools) GetPresets(ctx context.Context) ([]db.ScriptPresetWithScripts, error) {
	client, err := t.requireClient()
	if err != nil {
		return nil, err
	}
	return client.ScriptPresets.ListWithScripts(ctx)
}

func (t *Tools) IsPythonAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "python", "--version")
	return cmd.Run() == nil
}

func (t *Tools) CreatePreset(ctx context.Context, input CreateScriptPresetInput) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return contractError("Invalid preset name: name cannot be empty or only whitespace")
	}
	if len(input.ScriptIDs) == 0 {
		return contractError("No scripts selected")
	}
	if conflict, err := client.ScriptPresets.FindByName(ctx, name); err != nil {
		return err
	} else if conflict != nil {
		return contractError("Preset with same name already exists")
	}

	seen := make(map[string]struct{}, len(input.ScriptIDs))
	for _, scriptID := range input.ScriptIDs {
		if strings.TrimSpace(scriptID) == "" {
			return errors.New("invalid empty script id")
		}
		if _, ok := seen[scriptID]; ok {
			return fmt.Errorf("duplicate script id: %s", scriptID)
		}
		seen[scriptID] = struct{}{}
		row, findErr := client.Scripts.FindByID(ctx, scriptID)
		if findErr != nil {
			return findErr
		}
		if row == nil {
			return fmt.Errorf("script not found: %s", scriptID)
		}
	}

	presetID, err := newToolsID()
	if err != nil {
		return err
	}
	items := make([]db.ScriptPresetItemRow, len(input.ScriptIDs))
	for i, scriptID := range input.ScriptIDs {
		items[i] = db.ScriptPresetItemRow{PresetID: presetID, ScriptID: scriptID, Order: int64(i)}
	}
	return client.ScriptPresets.InsertSnapshot(ctx, db.ScriptPresetRow{ID: presetID, Name: name}, items)
}

func (t *Tools) DeletePreset(ctx context.Context, presetID string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	preset, err := client.ScriptPresets.FindByID(ctx, presetID)
	if err != nil {
		return err
	}
	if preset == nil {
		return contractError("Preset not found")
	}
	return client.ScriptPresets.Delete(ctx, presetID)
}

func (t *Tools) CancelRun() bool {
	if t == nil {
		return false
	}
	t.runMu.Lock()
	run := t.run
	if run != nil {
		run.cancel()
	}
	t.runMu.Unlock()
	if run != nil {
		t.emitFixToolLog("Cancelled...", false)
		return true
	}
	return false
}

func (t *Tools) SendInput(input string) bool {
	if t == nil {
		return false
	}
	t.runMu.Lock()
	run := t.run
	t.runMu.Unlock()
	if run == nil || run.executor == nil || !run.executor.sendInput(input) {
		if t.log != nil {
			t.log.Warn("Cannot send input: No active script running", "FixTool")
		}
		return false
	}
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Sent input: %q", input), "FixTool")
	}
	return true
}

func (t *Tools) RunScript(ctx context.Context, scriptID, destPath string) error {
	run, runCtx, err := t.beginScriptRun(ctx)
	if err != nil {
		return t.reportRunError(err)
	}
	defer t.finishScriptRun(run)

	client, err := t.requireClient()
	if err != nil {
		return t.reportRunError(err)
	}
	script, err := client.Scripts.FindByID(runCtx, scriptID)
	if err != nil {
		return t.reportRunError(err)
	}
	if script == nil {
		return t.reportRunError(contractError("Script not found"))
	}
	if err := t.validateRunDestination(destPath); err != nil {
		return t.reportRunError(err)
	}
	if script.Type == db.ScriptTypePython && !t.IsPythonAvailable(runCtx) {
		return t.reportRunError(contractError("Python is required to run Python fix tools. Install Python and make sure the python command is available."))
	}
	_ = t.runScriptSafe(runCtx, run, script, destPath, nil)
	return nil
}

func (t *Tools) RunPreset(ctx context.Context, presetID, destPath string) error {
	run, runCtx, err := t.beginScriptRun(ctx)
	if err != nil {
		return t.reportRunError(err)
	}
	defer t.finishScriptRun(run)

	client, err := t.requireClient()
	if err != nil {
		return t.reportRunError(err)
	}
	preset, err := client.ScriptPresets.FindByIDWithScripts(runCtx, presetID)
	if err != nil {
		return t.reportRunError(err)
	}
	if preset == nil {
		return t.reportRunError(contractError("Preset not found"))
	}
	if len(preset.Scripts) == 0 {
		return t.reportRunError(contractError("Preset has no scripts"))
	}
	if err := t.validateRunDestination(destPath); err != nil {
		return t.reportRunError(err)
	}

	sort.SliceStable(preset.Scripts, func(i, j int) bool { return preset.Scripts[i].Order < preset.Scripts[j].Order })
	scripts := make([]*db.ScriptRow, len(preset.Scripts))
	needsPython := false
	for i, item := range preset.Scripts {
		scripts[i], err = client.Scripts.FindByID(runCtx, item.ScriptID)
		if err != nil {
			return t.reportRunError(err)
		}
		needsPython = needsPython || scripts[i] != nil && scripts[i].Type == db.ScriptTypePython
	}
	if needsPython && !t.IsPythonAvailable(runCtx) {
		return t.reportRunError(contractError("Python is required to run Python fix tools. Install Python and make sure the python command is available."))
	}

	t.emitFixToolLog("Starting Preset: "+preset.Name, false)
	for i, script := range scripts {
		if runCtx.Err() != nil {
			t.emitFixToolLog("Preset execution aborted by user.", false)
			return nil
		}
		if script == nil {
			t.emitFixToolLog(fmt.Sprintf("Script not found (ID: %s), skipping...", preset.Scripts[i].ScriptID), false)
			continue
		}
		_ = t.runScriptSafe(runCtx, run, script, destPath, nil)
	}
	if runCtx.Err() == nil {
		t.emitFixToolLog("Preset Completed", false)
	}
	return nil
}

func (t *Tools) reportRunError(err error) error {
	t.logError(err, "FixTool")
	t.emitFixToolLog("Error: "+err.Error(), false)
	return nil
}

func (t *Tools) validateRunDestination(destPath string) error {
	info, err := os.Stat(destPath)
	if errors.Is(err, os.ErrNotExist) {
		return contractError("Destination path does not exist")
	}
	if err != nil {
		return fmt.Errorf("stat destination path: %w", err)
	}
	if !info.IsDir() {
		return contractError("Destination path is not a directory")
	}
	return nil
}

func (t *Tools) runScriptSafe(ctx context.Context, run *toolRun, script *db.ScriptRow, destPath string, args []string) bool {
	ext := "exe"
	if script.Type == db.ScriptTypePython {
		ext = "py"
	}
	tempName := fmt.Sprintf("%s-%d.%s", script.SHA256, time.Now().UnixMilli(), ext)
	scriptPath := filepath.Join(destPath, tempName)
	wrote := false
	defer func() {
		if wrote {
			if err := os.Remove(scriptPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.logError(fmt.Errorf("cleanup temp file: %w", err), "FixTool")
			}
		}
	}()

	data := script.Source
	if script.IsSrcZstd {
		decoded, err := decompressZstd(script.Source)
		if err != nil {
			t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
			return false
		}
		data = decoded
	} else {
		compressed, err := compressZstd(script.Source)
		if err != nil {
			t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
			return false
		}
		client, err := t.requireClient()
		if err != nil {
			t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
			return false
		}
		if err := client.Scripts.UpdateCompressedSource(ctx, script.ID, compressed, sha256Hex(compressed), int64(len(compressed))); err != nil {
			t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
			return false
		}
	}
	if err := os.WriteFile(scriptPath, data, 0o700); err != nil {
		t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
		return false
	}
	wrote = true
	t.emitFixToolLog("Running "+script.Name+"...", false)
	if err := run.executor.execute(ctx, scriptPath, script.Type, destPath, args); err != nil {
		if errors.Is(err, context.Canceled) {
			t.emitFixToolLog("Cancelled "+script.Name, false)
		} else {
			t.emitFixToolLog(fmt.Sprintf("Failed %s: %s", script.Name, err), false)
		}
		return false
	}
	t.emitFixToolLog("Completed "+script.Name, false)
	return true
}

func compressZstd(data []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	defer func() { _ = encoder.Close() }()
	return encoder.EncodeAll(data, nil), nil
}

func decompressZstd(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	defer decoder.Close()
	out, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress script: %w", err)
	}
	return out, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newToolsID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
