package mod

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/mod/namespace"
)

type namespaceIsolationManifest struct {
	Version    int               `json:"version"`
	InstanceID string            `json:"instanceId"`
	Mappings   map[string]string `json:"mappings"`
	Files      map[string]string `json:"files"`
}

func readNamespaceManifest(modPath string) (map[string]json.RawMessage, *namespaceIsolationManifest, []byte, error) {
	raw, _, err := readNamespaceFile(filepath.Join(modPath, "nhd.json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil || document == nil {
		return nil, nil, nil, fmt.Errorf("corrupt mod metadata: %s", modPath)
	}
	field, exists := document["namespaceIsolation"]
	if !exists {
		return document, nil, raw, nil
	}
	var manifest namespaceIsolationManifest
	if err := json.Unmarshal(field, &manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("corrupt namespace isolation metadata: %w", err)
	}
	id, err := hex.DecodeString(manifest.InstanceID)
	if manifest.Version != 1 || err != nil || len(id) != 16 || len(manifest.Mappings) == 0 || len(manifest.Files) == 0 {
		return nil, nil, nil, errors.New("unsupported or corrupt namespace isolation metadata")
	}
	for original, mapped := range manifest.Mappings {
		if original == "" || mapped == "" {
			return nil, nil, nil, errors.New("invalid namespace isolation mapping")
		}
	}
	for relative, fingerprint := range manifest.Files {
		if filepath.IsAbs(relative) || !pathWithin(modPath, filepath.Join(modPath, filepath.FromSlash(relative))) {
			return nil, nil, nil, errors.New("namespace isolation manifest contains an unsafe file path")
		}
		hash, err := hex.DecodeString(fingerprint)
		if err != nil || len(hash) != sha256.Size {
			return nil, nil, nil, errors.New("invalid namespace isolation file fingerprint")
		}
	}
	return document, &manifest, raw, nil
}

func namespaceCanonicalMapping(modPath string, files []namespaceIsolationFile) (map[string]string, error) {
	_, manifest, _, err := readNamespaceManifest(modPath)
	if err != nil {
		return nil, err
	}
	mapping := map[string]string{}
	if manifest != nil {
		for relative, fingerprint := range manifest.Files {
			verified := false
			for _, file := range files {
				if file.modPath == modPath && strings.EqualFold(file.relative, relative) {
					verified = file.document.Fingerprint == fingerprint
					break
				}
			}
			if !verified {
				return nil, fmt.Errorf("namespace isolation manifest no longer verifies %s", relative)
			}
		}
		for original, mapped := range manifest.Mappings {
			mapping[mapped] = original
		}
		return mapping, nil
	}
	for _, file := range files {
		if file.modPath != modPath || file.implicit || file.document.Namespace == "" {
			continue
		}
		name := file.document.Namespace
		canonical := name
		if canonical == "" {
			return nil, errors.New("isolated namespace has no canonical name")
		}
		mapping[name] = canonical
	}
	return mapping, nil
}

func namespaceChanges(plan namespaceIsolationPlan) ([]namespace.Change, error) {
	changes := []namespace.Change{}
	for _, modPath := range plan.mods {
		canonical, err := namespaceCanonicalMapping(modPath, plan.files)
		if err != nil {
			return nil, err
		}
		document, _, raw, err := readNamespaceManifest(modPath)
		if err != nil {
			return nil, err
		}
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		instance := strings.ReplaceAll(id.String(), "-", "")
		manifest := namespaceIsolationManifest{
			Version:    1,
			InstanceID: instance,
			Mappings:   map[string]string{},
			Files:      map[string]string{},
		}
		mapping := map[string]string{}
		for _, file := range plan.files {
			if file.modPath != modPath || file.implicit || file.document.Namespace == "" {
				continue
			}
			name := file.document.Namespace
			original := name
			for from, to := range canonical {
				if strings.EqualFold(from, name) {
					original = to
					break
				}
			}
			mapping[name] = original + "__nhd_" + instance
			manifest.Mappings[original] = mapping[name]
		}
		for _, file := range plan.files {
			if file.modPath != modPath {
				continue
			}
			after, err := namespace.Rewrite(file.content, mapping)
			if err != nil {
				return nil, fmt.Errorf("rewrite %s: %w", file.path, err)
			}
			parsed, err := namespace.Parse(after)
			if err != nil || parsed.Unsupported {
				return nil, errors.New("rewritten INI failed parsing")
			}
			manifest.Files[file.relative] = parsed.Fingerprint
			changes = append(
				changes,
				namespace.Change{
					ModPath:      modPath,
					RelativePath: file.relative,
					Before:       file.content,
					After:        after,
					Exists:       true,
					Info:         file.info,
				},
			)
		}
		// Preserve unknown fields in both nhd.json and namespaceIsolation.
		fields := map[string]json.RawMessage{}
		if field := document["namespaceIsolation"]; field != nil {
			if err := json.Unmarshal(field, &fields); err != nil {
				return nil, err
			}
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		var owned map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &owned); err != nil {
			return nil, err
		}
		for key, value := range owned {
			fields[key] = value
		}
		document["namespaceIsolation"], err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		after, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return nil, err
		}
		_, info, err := readNamespaceFile(filepath.Join(modPath, "nhd.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if info != nil && (!info.Mode().IsRegular() || isReparsePoint(info)) {
			return nil, errors.New("metadata is not a regular file")
		}
		changes = append(
			changes,
			namespace.Change{
				ModPath:      modPath,
				RelativePath: "nhd.json",
				Before:       raw,
				After:        after,
				Exists:       raw != nil,
				Info:         info,
			},
		)
	}
	return changes, nil
}

// Refresh only the manifest after a user rename or harmless edit. The current
// declaration wins; an unrecorded suffix is never interpreted as app ownership.
func namespaceManifestRefresh(modPath string, files []namespaceIsolationFile) ([]namespace.Change, error) {
	document, manifest, raw, err := readNamespaceManifest(modPath)
	if err != nil || manifest == nil {
		return nil, err
	}
	mappings := map[string]string{}
	fingerprints := map[string]string{}
	for _, file := range files {
		if file.modPath != modPath {
			continue
		}
		if file.document.Unsupported {
			return nil, fmt.Errorf("unsupported namespace INI %s", file.path)
		}
		fingerprints[file.relative] = file.document.Fingerprint
		name := file.document.Namespace
		if file.implicit || name == "" {
			continue
		}
		original := name
		for from, to := range manifest.Mappings {
			if strings.EqualFold(to, name) {
				original = from
				break
			}
		}
		mappings[original] = name
	}
	if len(mappings) == 0 || len(fingerprints) == 0 {
		return nil, errors.New("namespace manifest no longer has explicit INI declarations")
	}
	current, _ := json.Marshal(manifest)
	manifest.Mappings, manifest.Files = mappings, fingerprints
	next, _ := json.Marshal(manifest)
	if string(current) == string(next) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(document["namespaceIsolation"], &fields); err != nil {
		return nil, err
	}
	var owned map[string]json.RawMessage
	if err := json.Unmarshal(next, &owned); err != nil {
		return nil, err
	}
	for key, value := range owned {
		fields[key] = value
	}
	document["namespaceIsolation"], err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	after, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	_, info, err := readNamespaceFile(filepath.Join(modPath, "nhd.json"))
	if err != nil {
		return nil, err
	}
	return []namespace.Change{
		{ModPath: modPath, RelativePath: "nhd.json", Before: raw, After: after, Exists: true, Info: info},
	}, nil
}
