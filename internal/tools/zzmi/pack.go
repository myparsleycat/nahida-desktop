package zzmi

import (
	"archive/zip"
	"bytes"
	"crypto/sha1" //nolint:gosec // Git object IDs intentionally use SHA-1.
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	maxArchiveEntries = 5000
	maxRuleFileSize   = 1 << 20
	maxSelectedSize   = 8 << 20
)

//go:embed default_rules.json.zst
var embeddedFiles embed.FS

func LoadEmbedded() (*RulePack, error) {
	raw, err := embeddedFiles.ReadFile("default_rules.json.zst")
	if err != nil {
		return nil, err
	}
	return DecodePack(raw)
}

func EncodePack(pack RulePack) ([]byte, error) {
	if err := pack.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(pack)
	if err != nil {
		return nil, err
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return nil, err
	}
	defer func() { _ = encoder.Close() }()
	return encoder.EncodeAll(raw, nil), nil
}

func DecodePack(compressed []byte) (*RulePack, error) {
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(64<<20))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	raw, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress ZZMI rules: %w", err)
	}
	if len(raw) > 32<<20 {
		return nil, errors.New("ZZMI rule pack is too large")
	}
	var pack RulePack
	jsonDecoder := json.NewDecoder(bytes.NewReader(raw))
	jsonDecoder.UseNumber()
	if err := jsonDecoder.Decode(&pack); err != nil {
		return nil, fmt.Errorf("decode ZZMI rules: %w", err)
	}
	if err := pack.Validate(); err != nil {
		return nil, err
	}
	return &pack, nil
}

func CompileDirectory(root, tag, commit, published string) (*RulePack, error) {
	modules, err := filepath.Glob(filepath.Join(root, "Assets", "PlayerCharacterPYData", "*.py"))
	if err != nil {
		return nil, err
	}
	sort.Strings(modules)
	files := make(map[string][]byte, len(modules)+2)
	for _, filename := range modules {
		if strings.EqualFold(filepath.Base(filename), "__init__.py") {
			continue
		}
		data, readErr := os.ReadFile(filename)
		if readErr != nil {
			return nil, readErr
		}
		files[path.Join("Source Codes/Assets/PlayerCharacterPYData", filepath.Base(filename))] = data
	}
	for _, name := range []string{"Jane.remapper.py", "Dialyn.remapper.py"} {
		data, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, readErr
		}
		files[path.Join("Source Codes", name)] = data
	}
	return compileFiles(files, tag, commit, published)
}

func CompileZip(reader io.ReaderAt, size int64, tag, commit, published string, expectedBlobs map[string]string) (*RulePack, error) {
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return nil, fmt.Errorf("open ZZMI rule archive: %w", err)
	}
	if len(archive.File) > maxArchiveEntries {
		return nil, fmt.Errorf("ZZMI rule archive has too many entries: %d", len(archive.File))
	}
	files := map[string][]byte{}
	total := int64(0)
	for _, entry := range archive.File {
		name, ok := normalizedRuleArchivePath(entry.Name)
		if !ok {
			continue
		}
		if entry.UncompressedSize64 > maxRuleFileSize {
			return nil, fmt.Errorf("ZZMI rule file is too large: %s", name)
		}
		stream, openErr := entry.Open()
		if openErr != nil {
			return nil, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, maxRuleFileSize+1))
		closeErr := stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(data) > maxRuleFileSize {
			return nil, fmt.Errorf("ZZMI rule file is too large: %s", name)
		}
		total += int64(len(data))
		if total > maxSelectedSize {
			return nil, errors.New("selected ZZMI rules exceed the archive limit")
		}
		expected := expectedBlobs[name]
		if expected == "" {
			return nil, fmt.Errorf("git tree is missing %s", name)
		}
		if !strings.EqualFold(expected, gitBlobSHA(data)) {
			return nil, fmt.Errorf("git blob checksum mismatch for %s", name)
		}
		files[name] = data
	}
	for name := range expectedBlobs {
		if isSelectedRulePath(name) {
			if _, ok := files[name]; !ok {
				return nil, fmt.Errorf("ZZMI rule archive is missing %s", name)
			}
		}
	}
	return compileFiles(files, tag, commit, published)
}

func normalizedRuleArchivePath(name string) (string, bool) {
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", false
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 3 {
		return "", false
	}
	clean = strings.Join(parts[1:], "/")
	if isSelectedRulePath(clean) {
		return clean, true
	}
	return "", false
}

func isSelectedRulePath(clean string) bool {
	if clean == "Source Codes/Jane.remapper.py" || clean == "Source Codes/Dialyn.remapper.py" {
		return true
	}
	prefix := "Source Codes/Assets/PlayerCharacterPYData/"
	return strings.HasPrefix(clean, prefix) && path.Ext(clean) == ".py" && path.Base(clean) != "__init__.py"
}

func compileFiles(files map[string][]byte, tag, commit, published string) (*RulePack, error) {
	pack := NewPack(tag, commit, published)
	moduleNames := make([]string, 0, len(files))
	for name := range files {
		if strings.HasPrefix(name, "Source Codes/Assets/PlayerCharacterPYData/") {
			moduleNames = append(moduleNames, name)
		}
	}
	sort.Strings(moduleNames)
	for _, name := range moduleNames {
		commands, err := ParseCharacterModule(files[name])
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for hash, command := range commands {
			if _, exists := pack.HashCommands[hash]; exists {
				pack.Collisions++
			}
			pack.HashCommands[hash] = command
		}
	}
	if len(moduleNames) == 0 {
		return nil, errors.New("ZZMI archive has no character modules")
	}
	var err error
	pack.Jane, err = compileJane(files["Source Codes/Jane.remapper.py"])
	if err != nil {
		return nil, err
	}
	pack.Dialyn, err = compileDialyn(files["Source Codes/Dialyn.remapper.py"])
	if err != nil {
		return nil, err
	}
	if err := pack.Validate(); err != nil {
		return nil, err
	}
	return &pack, nil
}

func compileJane(source []byte) (RemapperRules, error) {
	values, err := ParseRemapper(source, "HAIR_MAPPINGS", "HAND_MAPPINGS", "POSITION_TO_BLEND", "STRIDE")
	if err != nil {
		return RemapperRules{}, fmt.Errorf("parse Jane remapper: %w", err)
	}
	hair, err := uintMapping(values["HAIR_MAPPINGS"])
	if err != nil {
		return RemapperRules{}, err
	}
	hand, err := uintMapping(values["HAND_MAPPINGS"])
	if err != nil {
		return RemapperRules{}, err
	}
	positions, err := stringMapping(values["POSITION_TO_BLEND"])
	if err != nil {
		return RemapperRules{}, err
	}
	stride, err := intValue(values["STRIDE"])
	if err != nil {
		return RemapperRules{}, err
	}
	hairHash, handHash := positions["33a09cfe"], positions["82e7c056"]
	if !isHash(hairHash) || !isHash(handHash) {
		return RemapperRules{}, errors.New("Jane remapper is missing the expected hair or hand mapping") //nolint:staticcheck // Product name starts the error.
	}
	valid := []string{strings.ToLower(hairHash), strings.ToLower(handHash)}
	return RemapperRules{Mapping: hair, Secondary: hand, PositionToBlend: positions, ValidHashes: valid, Stride: stride}, nil
}

func compileDialyn(source []byte) (RemapperRules, error) {
	values, err := ParseRemapper(source, "BLEND_MAPPING", "POSITION_TO_BLEND", "STRIDE", "VALID_BLEND_HASHES")
	if err != nil {
		return RemapperRules{}, fmt.Errorf("parse Dialyn remapper: %w", err)
	}
	mapping, err := uintMapping(values["BLEND_MAPPING"])
	if err != nil {
		return RemapperRules{}, err
	}
	positions, err := stringMapping(values["POSITION_TO_BLEND"])
	if err != nil {
		return RemapperRules{}, err
	}
	stride, err := intValue(values["STRIDE"])
	if err != nil {
		return RemapperRules{}, err
	}
	validRaw, ok := values["VALID_BLEND_HASHES"].([]any)
	if !ok {
		return RemapperRules{}, errors.New("Dialyn VALID_BLEND_HASHES is not a set") //nolint:staticcheck // Product name starts the error.
	}
	valid := make([]string, 0, len(validRaw))
	for _, item := range validRaw {
		value, ok := item.(string)
		if !ok || !isHash(value) {
			return RemapperRules{}, errors.New("invalid Dialyn blend hash")
		}
		valid = append(valid, strings.ToLower(value))
	}
	sort.Strings(valid)
	return RemapperRules{Mapping: mapping, PositionToBlend: positions, ValidHashes: valid, Stride: stride}, nil
}

func uintMapping(value any) (map[uint32]uint32, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("remapper mapping is not a dictionary")
	}
	result := make(map[uint32]uint32, len(raw))
	for key, item := range raw {
		from, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			return nil, err
		}
		to, err := intValue(item)
		if err != nil || to < 0 {
			return nil, errors.New("invalid remapper index")
		}
		result[uint32(from)] = uint32(to)
	}
	return result, nil
}

func stringMapping(value any) (map[string]string, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("remapper hash mapping is not a dictionary")
	}
	result := make(map[string]string, len(raw))
	for key, item := range raw {
		to, ok := item.(string)
		if !ok || !isHash(key) || !isHash(to) {
			return nil, errors.New("invalid remapper hash mapping")
		}
		result[strings.ToLower(key)] = strings.ToLower(to)
	}
	return result, nil
}

func intValue(value any) (int, error) {
	number, ok := value.(int64)
	if !ok || number > int64(^uint(0)>>1) {
		return 0, errors.New("literal is not an integer")
	}
	return int(number), nil
}

func gitBlobSHA(data []byte) string {
	hash := sha1.New() //nolint:gosec // Git object IDs intentionally use SHA-1.
	_, _ = fmt.Fprintf(hash, "blob %d%c", len(data), byte(0))
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
