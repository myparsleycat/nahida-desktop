package mod

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"nahida.live/desktop/internal/xxmi"
)

const (
	shaderFixesDirName           = "ShaderFixes"
	shaderFixesModMarkerFile     = ".nahida-shader-fixes.json"
	shaderFixesModMarkerVersion  = 1
	shaderFixesOwnerIndexVersion = 1
	shaderFixesModKeyAlphabet    = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz-"
	shaderFixesModKeyLength      = 21
)

type ShaderFixesProcessedFile struct {
	File          string `json:"file"`
	TargetPath    string `json:"targetPath"`
	TargetKey     string `json:"targetKey"`
	Hash          string `json:"hash"`
	ModKey        string `json:"modKey"`
	CreatedTarget bool   `json:"createdTarget"`
}

type shaderFixesModManifestFile struct {
	File       string `json:"file"`
	TargetPath string `json:"targetPath"`
	TargetKey  string `json:"targetKey"`
	Hash       string `json:"hash"`
}

type shaderFixesModManifest struct {
	Version int                          `json:"version"`
	ModKey  string                       `json:"modKey"`
	Files   []shaderFixesModManifestFile `json:"files"`
}

type shaderFixesOwnerIndexTarget struct {
	Hash   string   `json:"hash"`
	Owners []string `json:"owners"`
}

type shaderFixesOwnerIndex struct {
	Version int                                    `json:"version"`
	Targets map[string]shaderFixesOwnerIndexTarget `json:"targets"`
}

type shaderFixesFileCandidate struct {
	file       string
	sourcePath string
}

type shaderImporter struct {
	Key            string
	ImporterFolder string
}

type shaderGame struct {
	Game          string
	ModFolderPath string
	Importer      string
}

type shaderGlobCall struct {
	Pattern string
	Cwd     string
}

type shaderFixesError struct {
	err            error
	processedFiles []ShaderFixesProcessedFile
}

func (e *shaderFixesError) Error() string { return e.err.Error() }
func (e *shaderFixesError) Unwrap() error { return e.err }

type ShaderFixes struct {
	mu sync.Mutex

	getImporters func() []shaderImporter
	getGames     func() []shaderGame
	hashFile     func(string) (string, error)
	generateKey  func() string
	logError     func(error, string)

	globCalls             []shaderGlobCall
	ownerIndexWrites      int
	failOwnerIndexWriteOn int
}

func newShaderFixes() *ShaderFixes {
	return &ShaderFixes{
		hashFile:    hashShaderFixesFile,
		generateKey: newShaderFixesModKey,
	}
}

func (s *ShaderFixes) HandleShaders(modPath string, enable bool) ([]ShaderFixesProcessedFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleShadersLocked(modPath, enable)
}

func (s *ShaderFixes) RollbackEnabledShaders(modPath string, processedShaders []ShaderFixesProcessedFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rollbackError error
	globalShaderPath := s.getGlobalShaderFixesPath(modPath)
	var ownerIndex *shaderFixesOwnerIndex
	if globalShaderPath != "" {
		index, err := s.getShaderFixesOwnerIndex(modPath, globalShaderPath)
		if err != nil {
			rollbackError = err
		} else {
			ownerIndex = &index
		}
	}
	for i := len(processedShaders) - 1; i >= 0; i-- {
		file := processedShaders[i]
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					rollbackError = fmt.Errorf("%v", recovered)
				}
			}()
			var targetKey string
			if globalShaderPath != "" {
				if key := s.getShaderFixesOwnerTargetKey(globalShaderPath, file.TargetPath); key != nil {
					targetKey = *key
				}
			}
			var target *shaderFixesOwnerIndexTarget
			if ownerIndex != nil && targetKey != "" {
				if indexed, ok := ownerIndex.Targets[targetKey]; ok {
					copied := indexed
					target = &copied
				}
			}
			if targetKey != "" && target != nil && ownerIndex != nil {
				remaining := filterOwners(target.Owners, file.ModKey)
				if len(remaining) > 0 {
					ownerIndex.Targets[targetKey] = shaderFixesOwnerIndexTarget{Hash: target.Hash, Owners: remaining}
					return
				}
				delete(ownerIndex.Targets, targetKey)
			}
			if file.CreatedTarget {
				if _, err := os.Stat(file.TargetPath); err == nil {
					currentHash, err := s.hashFile(file.TargetPath)
					if err != nil {
						rollbackError = err
						return
					}
					if currentHash == file.Hash {
						if err := os.Remove(file.TargetPath); err != nil {
							rollbackError = err
						}
					}
				}
			}
		}()
	}
	if globalShaderPath != "" && ownerIndex != nil {
		if err := s.writeShaderFixesOwnerIndex(globalShaderPath, *ownerIndex); err != nil {
			rollbackError = err
		}
	}
	modKey := ""
	if len(processedShaders) > 0 {
		modKey = processedShaders[0].ModKey
	} else {
		modKey, _ = s.getShaderFixesModKey(modPath, false)
	}
	if modKey != "" {
		if err := s.DeleteModManifest(modPath); err != nil {
			rollbackError = err
		}
	}
	return rollbackError
}

func (s *ShaderFixes) DeleteModManifest(modPath string) error {
	err := os.Remove(s.getShaderFixesModManifestPath(modPath))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *ShaderFixes) handleShadersLocked(modPath string, enable bool) ([]ShaderFixesProcessedFile, error) {
	if !enable {
		return s.disableShaders(modPath)
	}
	shaderFiles, err := s.getShaderFixesFileCandidates(modPath)
	if err != nil {
		return nil, err
	}
	if len(shaderFiles) == 0 {
		return []ShaderFixesProcessedFile{}, nil
	}
	globalShaderPath := s.getGlobalShaderFixesPath(modPath)
	if globalShaderPath == "" {
		return []ShaderFixesProcessedFile{}, nil
	}
	processedFiles := []ShaderFixesProcessedFile{}
	if err := func() error {
		modKey, err := s.getShaderFixesModKey(modPath, true)
		if err != nil {
			return err
		}
		ownerIndex, err := s.getShaderFixesOwnerIndex(modPath, globalShaderPath)
		if err != nil {
			return err
		}
		manifest := shaderFixesModManifest{Version: shaderFixesModMarkerVersion, ModKey: modKey, Files: []shaderFixesModManifestFile{}}
		if err := os.MkdirAll(globalShaderPath, 0o755); err != nil {
			return err
		}
		for _, candidate := range shaderFiles {
			target := filepath.Join(globalShaderPath, filepath.FromSlash(candidate.file))
			hash, err := s.hashFile(candidate.sourcePath)
			if err != nil {
				return err
			}
			targetKey := s.getShaderFixesTargetKey(target)
			_, statErr := os.Stat(target)
			targetExists := statErr == nil
			if targetExists {
				currentHash, err := s.hashFile(target)
				if err != nil {
					return err
				}
				if currentHash != hash {
					continue
				}
			} else {
				if err := copyShaderFixesFile(candidate.sourcePath, target); err != nil {
					return err
				}
			}
			manifestFile := shaderFixesModManifestFile{
				File: candidate.file, TargetPath: target, TargetKey: targetKey, Hash: hash,
			}
			manifest.Files = append(manifest.Files, manifestFile)
			processedFiles = append(processedFiles, ShaderFixesProcessedFile{
				File: candidate.file, TargetPath: target, TargetKey: targetKey, Hash: hash,
				ModKey: modKey, CreatedTarget: !targetExists,
			})
			ownerTargetKey := s.normalizeShaderFixesOwnerTargetKey(candidate.file)
			if ownerTargetKey == nil {
				continue
			}
			indexed, ok := ownerIndex.Targets[*ownerTargetKey]
			owners := []string{modKey}
			if ok && indexed.Hash == hash {
				owners = uniqueOwners(append(indexed.Owners, modKey))
			}
			if ownerIndex.Targets == nil {
				ownerIndex.Targets = map[string]shaderFixesOwnerIndexTarget{}
			}
			ownerIndex.Targets[*ownerTargetKey] = shaderFixesOwnerIndexTarget{Hash: hash, Owners: owners}
			if err := s.writeShaderFixesModManifest(modPath, manifest); err != nil {
				return err
			}
		}
		if len(manifest.Files) > 0 {
			if err := s.writeShaderFixesOwnerIndex(globalShaderPath, ownerIndex); err != nil {
				return err
			}
			return s.writeShaderFixesModManifest(modPath, manifest)
		}
		return s.DeleteModManifest(modPath)
	}(); err != nil {
		return nil, &shaderFixesError{err: err, processedFiles: processedFiles}
	}
	return processedFiles, nil
}

func (s *ShaderFixes) disableShaders(modPath string) ([]ShaderFixesProcessedFile, error) {
	manifest, err := s.readShaderFixesModManifest(modPath)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return []ShaderFixesProcessedFile{}, nil
	}
	processedFiles := []ShaderFixesProcessedFile{}
	if err := func() error {
		for _, group := range s.groupShaderFixesManifestFilesByImporter(manifest.Files) {
			currentOwnerIndex, err := s.getShaderFixesOwnerIndex(modPath, group.globalShaderPath)
			if err != nil {
				return err
			}
			missingOwner := false
			for _, file := range group.files {
				targetKey := s.getShaderFixesOwnerTargetKey(group.globalShaderPath, file.TargetPath)
				if targetKey == nil {
					continue
				}
				target, ok := currentOwnerIndex.Targets[*targetKey]
				if !ok || !containsOwner(target.Owners, manifest.ModKey) {
					missingOwner = true
					break
				}
			}
			ownerIndex := currentOwnerIndex
			if missingOwner {
				rebuilt, err := s.rebuildShaderFixesOwnerIndex(modPath, group.globalShaderPath)
				if err != nil {
					return err
				}
				ownerIndex = rebuilt
			}
			for _, file := range group.files {
				targetKey := s.getShaderFixesOwnerTargetKey(group.globalShaderPath, file.TargetPath)
				if targetKey == nil {
					continue
				}
				target, ok := ownerIndex.Targets[*targetKey]
				if !ok || !containsOwner(target.Owners, manifest.ModKey) {
					continue
				}
				remaining := filterOwners(target.Owners, manifest.ModKey)
				if len(remaining) > 0 {
					ownerIndex.Targets[*targetKey] = shaderFixesOwnerIndexTarget{Hash: target.Hash, Owners: remaining}
					continue
				}
				if _, statErr := os.Stat(file.TargetPath); statErr == nil {
					currentHash, err := s.hashFile(file.TargetPath)
					if err != nil {
						return err
					}
					if currentHash == file.Hash {
						processedFiles = append(processedFiles, ShaderFixesProcessedFile{
							File: file.File, TargetPath: file.TargetPath, TargetKey: file.TargetKey, Hash: file.Hash,
							ModKey: manifest.ModKey, CreatedTarget: true,
						})
						if err := os.Remove(file.TargetPath); err != nil {
							return err
						}
					}
				}
				delete(ownerIndex.Targets, *targetKey)
			}
			if err := s.writeShaderFixesOwnerIndex(group.globalShaderPath, ownerIndex); err != nil {
				return err
			}
		}
		return s.DeleteModManifest(modPath)
	}(); err != nil {
		return nil, &shaderFixesError{err: err, processedFiles: processedFiles}
	}
	return processedFiles, nil
}

func (s *ShaderFixes) getGlobalShaderFixesPath(modPath string) string {
	importers := s.importers()
	if importer := s.getModImporter(modPath, importers); importer != nil {
		return filepath.Join(importer.ImporterFolder, shaderFixesDirName)
	}
	matched := shaderGame{}
	found := false
	for _, game := range s.games() {
		if isSameOrChildPath(game.ModFolderPath, modPath) {
			matched = game
			found = true
			break
		}
	}
	if !found {
		return ""
	}
	keys := make([]string, 0, len(importers))
	for _, importer := range importers {
		keys = append(keys, importer.Key)
	}
	importerKey := matched.Importer
	if importerKey == "" {
		if matchedKey := xxmi.GetMatchingImporter(matched.Game, keys); matchedKey != nil {
			importerKey = *matchedKey
		}
	}
	for _, importer := range importers {
		if strings.EqualFold(importer.Key, importerKey) {
			return filepath.Join(importer.ImporterFolder, shaderFixesDirName)
		}
	}
	return ""
}

func (s *ShaderFixes) getModImporter(modPath string, importers []shaderImporter) *shaderImporter {
	byKey := make(map[string]shaderImporter, len(importers))
	for _, importer := range importers {
		byKey[strings.ToUpper(importer.Key)] = importer
	}
	current, err := filepath.Abs(modPath)
	if err != nil {
		current = filepath.Clean(modPath)
	}
	parent := filepath.Dir(current)
	for parent != current {
		if importer, ok := byKey[strings.ToUpper(filepath.Base(parent))]; ok {
			copied := importer
			return &copied
		}
		current = parent
		parent = filepath.Dir(current)
	}
	return nil
}

func (s *ShaderFixes) getShaderFixesManifestSearchRoots(modPath, globalShaderPath string) []string {
	roots := map[string]string{}
	addRoot := func(root string) {
		resolved, err := filepath.Abs(root)
		if err != nil {
			resolved = filepath.Clean(root)
		}
		roots[normalizeModPath(resolved)] = resolved
	}
	importers := s.importers()
	for _, game := range s.games() {
		keys := make([]string, 0, len(importers))
		for _, importer := range importers {
			keys = append(keys, importer.Key)
		}
		importerKey := game.Importer
		if importerKey == "" {
			if matched := xxmi.GetMatchingImporter(game.Game, keys); matched != nil {
				importerKey = *matched
			}
		}
		for _, importer := range importers {
			if !strings.EqualFold(importer.Key, importerKey) {
				continue
			}
			if normalizeModPath(filepath.Join(importer.ImporterFolder, shaderFixesDirName)) == normalizeModPath(globalShaderPath) {
				addRoot(game.ModFolderPath)
			}
		}
	}
	conventionalModsPath := filepath.Join(filepath.Dir(globalShaderPath), "Mods")
	if isSameOrChildPath(conventionalModsPath, modPath) {
		addRoot(conventionalModsPath)
	}
	if len(roots) == 0 {
		addRoot(filepath.Dir(modPath))
	}
	existing := make([]string, 0, len(roots))
	for _, root := range roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			existing = append(existing, root)
		}
	}
	sort.Slice(existing, func(i, j int) bool { return len(existing[i]) < len(existing[j]) })
	filtered := make([]string, 0, len(existing))
	for index, root := range existing {
		nested := false
		for _, parent := range existing[:index] {
			if isSameOrChildPath(parent, root) {
				nested = true
				break
			}
		}
		if !nested {
			filtered = append(filtered, root)
		}
	}
	return filtered
}

func (s *ShaderFixes) rebuildShaderFixesOwnerIndex(modPath, globalShaderPath string) (shaderFixesOwnerIndex, error) {
	manifestPaths := map[string]string{}
	current := s.getShaderFixesModManifestPath(modPath)
	if _, err := os.Stat(current); err == nil {
		manifestPaths[normalizeModPath(current)] = current
	}
	for _, root := range s.getShaderFixesManifestSearchRoots(modPath, globalShaderPath) {
		matches, err := s.glob(filepath.ToSlash("**/"+shaderFixesModMarkerFile), root, shaderGlobOptions{
			onlyFiles: true, ignoreShaderFixes: true,
		})
		if err != nil {
			return shaderFixesOwnerIndex{}, err
		}
		for _, match := range matches {
			manifestPath := filepath.Join(root, match)
			manifestPaths[normalizeModPath(manifestPath)] = manifestPath
		}
	}
	index := shaderFixesOwnerIndex{Version: shaderFixesOwnerIndexVersion, Targets: map[string]shaderFixesOwnerIndexTarget{}}
	for _, manifestPath := range manifestPaths {
		manifest, err := s.readShaderFixesModManifestFile(manifestPath)
		if err != nil || manifest == nil {
			continue
		}
		for _, file := range manifest.Files {
			targetKey := s.getShaderFixesOwnerTargetKey(globalShaderPath, file.TargetPath)
			if targetKey == nil {
				continue
			}
			target, ok := index.Targets[*targetKey]
			if !ok {
				index.Targets[*targetKey] = shaderFixesOwnerIndexTarget{Hash: file.Hash, Owners: []string{manifest.ModKey}}
				continue
			}
			if target.Hash == file.Hash && !containsOwner(target.Owners, manifest.ModKey) {
				target.Owners = append(target.Owners, manifest.ModKey)
				index.Targets[*targetKey] = target
			}
		}
	}
	if err := s.writeShaderFixesOwnerIndex(globalShaderPath, index); err != nil {
		return shaderFixesOwnerIndex{}, err
	}
	return index, nil
}

func (s *ShaderFixes) getShaderFixesOwnerIndex(modPath, globalShaderPath string) (shaderFixesOwnerIndex, error) {
	if index, err := s.readShaderFixesOwnerIndex(globalShaderPath); err != nil {
		return shaderFixesOwnerIndex{}, err
	} else if index != nil {
		return *index, nil
	}
	return s.rebuildShaderFixesOwnerIndex(modPath, globalShaderPath)
}

func (s *ShaderFixes) getShaderFixesModKey(modPath string, create bool) (string, error) {
	manifest, err := s.readShaderFixesModManifest(modPath)
	if err != nil {
		return "", err
	}
	if manifest != nil {
		return manifest.ModKey, nil
	}
	if !create {
		return "", nil
	}
	modKey := s.nextModKey()
	if err := s.writeShaderFixesModManifest(modPath, shaderFixesModManifest{
		Version: shaderFixesModMarkerVersion, ModKey: modKey, Files: []shaderFixesModManifestFile{},
	}); err != nil {
		return "", err
	}
	return modKey, nil
}

func (s *ShaderFixes) getShaderFixesFileCandidates(modPath string) ([]shaderFixesFileCandidate, error) {
	directories, err := s.glob(filepath.ToSlash("**/"+shaderFixesDirName), modPath, shaderGlobOptions{onlyDirs: true, caseInsensitive: true})
	if err != nil {
		return nil, err
	}
	if len(directories) == 0 {
		return nil, nil
	}
	unique := uniqueStrings(directories)
	sort.Slice(unique, func(i, j int) bool {
		aRoot := strings.EqualFold(normalizeShaderFixesRelativePath(unique[i]), shaderFixesDirName)
		bRoot := strings.EqualFold(normalizeShaderFixesRelativePath(unique[j]), shaderFixesDirName)
		if aRoot && !bRoot {
			return true
		}
		if bRoot && !aRoot {
			return false
		}
		return unique[i] < unique[j]
	})
	var candidates []shaderFixesFileCandidate
	for _, directory := range unique {
		shaderPath := filepath.Join(modPath, directory)
		files, err := s.glob("**/*", shaderPath, shaderGlobOptions{onlyFiles: true, ignoreMarkerFile: true})
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, file := range files {
			candidates = append(candidates, shaderFixesFileCandidate{
				file:       normalizeShaderFixesRelativePath(file),
				sourcePath: filepath.Join(shaderPath, file),
			})
		}
	}
	return candidates, nil
}

type shaderManifestGroup struct {
	globalShaderPath string
	files            []shaderFixesModManifestFile
}

func (s *ShaderFixes) groupShaderFixesManifestFilesByImporter(files []shaderFixesModManifestFile) []shaderManifestGroup {
	index := map[string]int{}
	var groups []shaderManifestGroup
	for _, file := range files {
		global := s.getShaderFixesPathFromManifestFile(file)
		if global == "" {
			continue
		}
		key := normalizeModPath(global)
		if at, ok := index[key]; ok {
			groups[at].files = append(groups[at].files, file)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, shaderManifestGroup{globalShaderPath: global, files: []shaderFixesModManifestFile{file}})
	}
	return groups
}

func (s *ShaderFixes) getShaderFixesPathFromManifestFile(file shaderFixesModManifestFile) string {
	relativePath := s.normalizeShaderFixesOwnerTargetKey(file.File)
	if relativePath == nil {
		return ""
	}
	globalShaderPath := filepath.Clean(file.TargetPath)
	for range strings.Split(*relativePath, "/") {
		globalShaderPath = filepath.Dir(globalShaderPath)
	}
	if !strings.EqualFold(filepath.Base(globalShaderPath), shaderFixesDirName) {
		return ""
	}
	if normalizeModPath(filepath.Join(globalShaderPath, filepath.FromSlash(*relativePath))) != normalizeModPath(mustAbs(file.TargetPath)) {
		return ""
	}
	return globalShaderPath
}

func (s *ShaderFixes) getShaderFixesModManifestPath(modPath string) string {
	return filepath.Join(modPath, shaderFixesModMarkerFile)
}

func (s *ShaderFixes) getShaderFixesOwnerIndexPath(globalShaderPath string) string {
	return filepath.Join(globalShaderPath, shaderFixesModMarkerFile)
}

func (s *ShaderFixes) getShaderFixesTargetKey(targetPath string) string {
	return hashShaderFixesString(normalizeModPath(mustAbs(targetPath)))
}

func (s *ShaderFixes) normalizeShaderFixesOwnerTargetKey(targetPath string) *string {
	normalized := normalizeShaderFixesRelativePath(targetPath)
	if normalized == "" || filepath.IsAbs(targetPath) || shaderFixesPathHasParentSegment(normalized) ||
		strings.EqualFold(normalized, shaderFixesModMarkerFile) {
		return nil
	}
	lower := strings.ToLower(normalized)
	return &lower
}

func shaderFixesPathHasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func (s *ShaderFixes) getShaderFixesOwnerTargetKey(globalShaderPath, targetPath string) *string {
	if !isSameOrChildPath(globalShaderPath, targetPath) {
		return nil
	}
	relative, err := filepath.Rel(globalShaderPath, targetPath)
	if err != nil {
		return nil
	}
	return s.normalizeShaderFixesOwnerTargetKey(relative)
}

func (s *ShaderFixes) validateShaderFixesModManifest(raw []byte) *shaderFixesModManifest {
	var manifest shaderFixesModManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	if manifest.Version != shaderFixesModMarkerVersion || manifest.ModKey == "" {
		return nil
	}
	files := make([]shaderFixesModManifestFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.File == "" || file.TargetPath == "" || file.TargetKey == "" || file.Hash == "" {
			continue
		}
		files = append(files, file)
	}
	return &shaderFixesModManifest{Version: shaderFixesModMarkerVersion, ModKey: manifest.ModKey, Files: files}
}

func (s *ShaderFixes) readShaderFixesModManifestFile(manifestPath string) (*shaderFixesModManifest, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		s.log(err, "Mod:readShaderFixesModManifestFile:"+manifestPath)
		return nil, nil
	}
	return s.validateShaderFixesModManifest(raw), nil
}

func (s *ShaderFixes) readShaderFixesModManifest(modPath string) (*shaderFixesModManifest, error) {
	return s.readShaderFixesModManifestFile(s.getShaderFixesModManifestPath(modPath))
}

func (s *ShaderFixes) writeShaderFixesModManifest(modPath string, manifest shaderFixesModManifest) error {
	return writeShaderFixesJSON(s.getShaderFixesModManifestPath(modPath), manifest)
}

func (s *ShaderFixes) validateShaderFixesOwnerIndex(raw []byte) *shaderFixesOwnerIndex {
	var candidate struct {
		Version int                        `json:"version"`
		Targets map[string]json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return nil
	}
	if candidate.Version != shaderFixesOwnerIndexVersion || candidate.Targets == nil {
		return nil
	}
	targets := map[string]shaderFixesOwnerIndexTarget{}
	for targetKey, rawTarget := range candidate.Targets {
		var target shaderFixesOwnerIndexTarget
		if err := json.Unmarshal(rawTarget, &target); err != nil || target.Hash == "" || target.Owners == nil {
			return nil
		}
		normalized := s.normalizeShaderFixesOwnerTargetKey(targetKey)
		owners := uniqueOwners(target.Owners)
		if normalized == nil || len(owners) == 0 {
			return nil
		}
		targets[*normalized] = shaderFixesOwnerIndexTarget{Hash: target.Hash, Owners: owners}
	}
	if len(targets) != len(candidate.Targets) {
		return nil
	}
	return &shaderFixesOwnerIndex{Version: shaderFixesOwnerIndexVersion, Targets: targets}
}

func (s *ShaderFixes) readShaderFixesOwnerIndex(globalShaderPath string) (*shaderFixesOwnerIndex, error) {
	indexPath := s.getShaderFixesOwnerIndexPath(globalShaderPath)
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		s.log(err, "Mod:readShaderFixesOwnerIndex:"+indexPath)
		return nil, nil
	}
	index := s.validateShaderFixesOwnerIndex(raw)
	if index != nil {
		return index, nil
	}
	s.log(errors.New("INVALID_SHADER_FIXES_OWNER_INDEX"), "Mod:readShaderFixesOwnerIndex:"+indexPath)
	return nil, nil
}

func (s *ShaderFixes) writeShaderFixesOwnerIndex(globalShaderPath string, index shaderFixesOwnerIndex) error {
	s.ownerIndexWrites++
	if s.failOwnerIndexWriteOn > 0 && s.ownerIndexWrites == s.failOwnerIndexWriteOn {
		return errors.New("OWNER_INDEX_WRITE_FAILED")
	}
	if err := os.MkdirAll(globalShaderPath, 0o755); err != nil {
		return err
	}
	if index.Targets == nil {
		index.Targets = map[string]shaderFixesOwnerIndexTarget{}
	}
	return writeShaderFixesJSON(s.getShaderFixesOwnerIndexPath(globalShaderPath), index)
}

type shaderGlobOptions struct {
	onlyFiles         bool
	onlyDirs          bool
	caseInsensitive   bool
	ignoreShaderFixes bool
	ignoreMarkerFile  bool
}

func (s *ShaderFixes) glob(pattern, cwd string, opts shaderGlobOptions) ([]string, error) {
	s.globCalls = append(s.globCalls, shaderGlobCall{Pattern: pattern, Cwd: cwd})
	info, err := os.Stat(cwd)
	switch {
	case err == nil && info.IsDir():
	case err == nil || os.IsNotExist(err):
		return nil, nil
	default:
		return nil, err
	}
	wantBase := strings.TrimPrefix(strings.ReplaceAll(pattern, `\`, "/"), "**/")
	var matches []string
	walkErr := filepath.WalkDir(cwd, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if opts.ignoreShaderFixes && entry.IsDir() && strings.EqualFold(entry.Name(), shaderFixesDirName) {
			return filepath.SkipDir
		}
		if opts.onlyDirs && !entry.IsDir() {
			return nil
		}
		if opts.onlyFiles && entry.IsDir() {
			return nil
		}
		if opts.ignoreMarkerFile && !entry.IsDir() && entry.Name() == shaderFixesModMarkerFile {
			return nil
		}
		name := entry.Name()
		if opts.caseInsensitive {
			if !strings.EqualFold(name, wantBase) && !strings.EqualFold(filepath.ToSlash(rel), wantBase) {
				return nil
			}
		} else if name != wantBase && filepath.ToSlash(rel) != wantBase && pattern != "**/*" {
			return nil
		}
		if pattern == "**/*" && entry.IsDir() {
			return nil
		}
		matches = append(matches, rel)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return matches, nil
}

func (s *ShaderFixes) importers() []shaderImporter {
	if s == nil || s.getImporters == nil {
		return nil
	}
	return s.getImporters()
}

func (s *ShaderFixes) games() []shaderGame {
	if s == nil || s.getGames == nil {
		return nil
	}
	return s.getGames()
}

func (s *ShaderFixes) log(err error, where string) {
	if s != nil && s.logError != nil && err != nil {
		s.logError(err, where)
	}
}

func (s *ShaderFixes) nextModKey() string {
	if s != nil && s.generateKey != nil {
		return s.generateKey()
	}
	return newShaderFixesModKey()
}

func hashShaderFixesFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashShaderFixesBytes(raw), nil
}

func hashShaderFixesBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func hashShaderFixesString(value string) string {
	return hashShaderFixesBytes([]byte(value))
}

func newShaderFixesModKey() string {
	buf := make([]byte, shaderFixesModKeyLength)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", buf)
	}
	out := make([]byte, shaderFixesModKeyLength)
	for i, b := range buf {
		out[i] = shaderFixesModKeyAlphabet[int(b)%len(shaderFixesModKeyAlphabet)]
	}
	return string(out)
}

func writeShaderFixesJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	mode := os.FileMode(0o666)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return atomicWriteFile(path, raw, mode)
}

func copyShaderFixesFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o666)
}

func normalizeModPath(modPath string) string {
	return strings.ToLower(filepath.Clean(modPath))
}

func isSameOrChildPath(parentPath, targetPath string) bool {
	parent := normalizeModPath(mustAbs(parentPath))
	target := normalizeModPath(mustAbs(targetPath))
	rel, err := filepath.Rel(parent, target)
	if err != nil {
		return false
	}
	return rel == "." || rel == "" || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func normalizeShaderFixesRelativePath(targetPath string) string {
	parts := strings.FieldsFunc(targetPath, func(r rune) bool { return r == '\\' || r == '/' })
	return strings.Join(parts, "/")
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

func uniqueOwners(owners []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner == "" {
			continue
		}
		if _, ok := seen[owner]; ok {
			continue
		}
		seen[owner] = struct{}{}
		out = append(out, owner)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsOwner(owners []string, owner string) bool {
	for _, candidate := range owners {
		if candidate == owner {
			return true
		}
	}
	return false
}

func filterOwners(owners []string, skip string) []string {
	out := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner != skip {
			out = append(out, owner)
		}
	}
	return out
}

func processedFilesFromError(err error) []ShaderFixesProcessedFile {
	var shaderErr *shaderFixesError
	if errors.As(err, &shaderErr) {
		return shaderErr.processedFiles
	}
	return nil
}
