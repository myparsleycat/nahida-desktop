package zzmi

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion       = 1
	EmbeddedTag         = "zzz-mod-fixer-v2.1.0"
	EmbeddedCommit      = "f34da9cbc5d998cdecc9c4ba1d6e2a0302cf5adf"
	ToolHash            = "hash"
	ToolJane            = "jane"
	ToolDialyn          = "dialyn"
	DefaultBufferStride = 32
)

type Command struct {
	Op     string         `json:"op"`
	Args   []any          `json:"args,omitempty"`
	Kwargs map[string]any `json:"kwargs,omitempty"`
}

type RemapperRules struct {
	Mapping         map[uint32]uint32 `json:"mapping"`
	Secondary       map[uint32]uint32 `json:"secondary,omitempty"`
	PositionToBlend map[string]string `json:"positionToBlend"`
	ValidHashes     []string          `json:"validHashes"`
	Stride          int               `json:"stride"`
}

type RulePack struct {
	SchemaVersion      int                  `json:"schemaVersion"`
	UpstreamTag        string               `json:"upstreamTag"`
	CommitSHA          string               `json:"commitSHA"`
	ReleasePublishedAt string               `json:"releasePublishedAt,omitempty"`
	GeneratedAt        string               `json:"generatedAt"`
	HashCommands       map[string][]Command `json:"hashCommands"`
	Jane               RemapperRules        `json:"jane"`
	Dialyn             RemapperRules        `json:"dialyn"`
	Collisions         int                  `json:"collisions"`
}

type Change struct {
	Path string
	Kind string
	Data []byte
}

type Result struct {
	ScannedINI   int
	ChangedINI   int
	ChangedBUF   int
	SkippedFiles int
	Warnings     []string
	Changes      []Change
}

type Logger func(message string)

func (p *RulePack) Validate() error {
	if p == nil {
		return errors.New("nil ZZMI rule pack")
	}
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported ZZMI rule schema %d", p.SchemaVersion)
	}
	if strings.TrimSpace(p.UpstreamTag) == "" || len(p.CommitSHA) != 40 {
		return errors.New("ZZMI rule pack is missing upstream identity")
	}
	if len(p.HashCommands) == 0 {
		return errors.New("ZZMI rule pack contains no hash commands")
	}
	for hash, commands := range p.HashCommands {
		if !isHash(hash) || len(commands) == 0 {
			return fmt.Errorf("invalid ZZMI hash command entry %q", hash)
		}
		for _, command := range commands {
			if _, ok := allowedOperations[command.Op]; !ok {
				return fmt.Errorf("unsupported ZZMI operation %q", command.Op)
			}
			if err := validateCommand(command); err != nil {
				return fmt.Errorf("invalid ZZMI operation %s for %s: %w", command.Op, hash, err)
			}
		}
	}
	if err := validateRemapper("Jane", p.Jane); err != nil {
		return err
	}
	return validateRemapper("Dialyn", p.Dialyn)
}

func validateRemapper(name string, rules RemapperRules) error {
	if rules.Stride != DefaultBufferStride || len(rules.Mapping) == 0 || len(rules.ValidHashes) == 0 {
		return fmt.Errorf("invalid %s remapper rules", name)
	}
	for _, hash := range rules.ValidHashes {
		if !isHash(hash) {
			return fmt.Errorf("invalid %s remapper hash %q", name, hash)
		}
	}
	return nil
}

func NewPack(tag, commit, published string) RulePack {
	return RulePack{
		SchemaVersion: SchemaVersion, UpstreamTag: tag, CommitSHA: commit,
		ReleasePublishedAt: published, GeneratedAt: published,
		HashCommands: make(map[string][]Command),
	}
}

func validateCommand(command Command) error {
	exactArgs := func(count int) error {
		if len(command.Args) != count || len(command.Kwargs) != 0 {
			return fmt.Errorf("expected %d positional arguments", count)
		}
		return nil
	}
	switch command.Op {
	case "log":
		if len(command.Args) == 0 || len(command.Kwargs) != 0 {
			return errors.New("log requires text")
		}
	case "update_hash":
		if err := exactArgs(1); err != nil {
			return err
		}
		value, ok := command.Args[0].(string)
		if !ok || !isHash(value) {
			return errors.New("update_hash requires one hash")
		}
	case "add_section_if_missing":
		if len(command.Args) < 2 || len(command.Args) > 3 || len(command.Kwargs) != 0 {
			return errors.New("add_section_if_missing requires two or three arguments")
		}
		if !validHashValue(command.Args[0]) {
			return errors.New("invalid equivalent hash list")
		}
		if _, ok := command.Args[1].(string); !ok {
			return errors.New("section title is not a string")
		}
		if len(command.Args) == 3 {
			if _, ok := command.Args[2].(string); !ok {
				return errors.New("section content is not a string")
			}
		}
	case "multiply_section_if_missing":
		if err := exactArgs(2); err != nil {
			return err
		}
		if !validHashValue(command.Args[0]) {
			return errors.New("invalid equivalent hash list")
		}
		if _, ok := command.Args[1].(string); !ok {
			return errors.New("section title is not a string")
		}
	case "add_ib_check_if_missing":
		if err := exactArgs(0); err != nil {
			return err
		}
	case "zzz_12_shrink_texcoord_color":
		if err := exactArgs(1); err != nil {
			return err
		}
		if _, ok := command.Args[0].(string); !ok {
			return errors.New("fix id is not a string")
		}
	case "zzz_13_remap_texcoord":
		if err := exactArgs(3); err != nil {
			return err
		}
		if _, ok := command.Args[0].(string); !ok || !validStringSequence(command.Args[1]) || !validStringSequence(command.Args[2]) {
			return errors.New("invalid texcoord remap arguments")
		}
	case "update_buffer_blend_indices":
		if err := exactArgs(3); err != nil {
			return err
		}
		hash, ok := command.Args[0].(string)
		if !ok || !isHash(hash) || !validIntegerSequence(command.Args[1]) || !validIntegerSequence(command.Args[2]) {
			return errors.New("invalid blend remap arguments")
		}
	case "transfer_indexed_sections":
		if len(command.Args) != 0 || len(command.Kwargs) != 2 || !validStringSequence(command.Kwargs["src_indices"]) || !validStringSequence(command.Kwargs["trg_indices"]) {
			return errors.New("invalid indexed-section transfer")
		}
	}
	return nil
}

func validHashValue(value any) bool {
	if hash, ok := value.(string); ok {
		return isHash(hash)
	}
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		hash, ok := item.(string)
		if !ok || !isHash(hash) {
			return false
		}
	}
	return true
}

func validStringSequence(value any) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}
func validIntegerSequence(value any) bool {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return false
	}
	for _, item := range items {
		switch item.(type) {
		case int64, json.Number:
		default:
			return false
		}
	}
	return true
}

func SortedHashes(commands map[string][]Command) []string {
	hashes := make([]string, 0, len(commands))
	for hash := range commands {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return hashes
}

func isHash(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

var allowedOperations = map[string]struct{}{
	"log": {}, "update_hash": {}, "add_section_if_missing": {},
	"multiply_section_if_missing": {}, "add_ib_check_if_missing": {},
	"zzz_12_shrink_texcoord_color": {}, "zzz_13_remap_texcoord": {},
	"update_buffer_blend_indices": {}, "transfer_indexed_sections": {},
}
