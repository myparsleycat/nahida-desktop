package agent

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

//go:embed skills/*/SKILL.md skills/*/references/*
var builtInSkills embed.FS

type SkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Error       string `json:"error,omitempty"`
}

type skillRecord struct {
	SkillView
	root     fs.FS
	dir      string
	userRoot string
}

type skillCatalog struct {
	mu       sync.RWMutex
	userPath string
	records  map[string]skillRecord
}

func newSkillCatalog(userPath string) *skillCatalog {
	return &skillCatalog{userPath: userPath, records: make(map[string]skillRecord)}
}

func (c *skillCatalog) Reload() []SkillView {
	records := make(map[string]skillRecord)
	loadSkillsFromFS(records, builtInSkills, "skills", "built-in", "")
	if err := os.MkdirAll(c.userPath, 0o700); err == nil {
		loadUserSkills(records, c.userPath)
	}
	c.mu.Lock()
	c.records = records
	c.mu.Unlock()
	return c.List()
}

func (c *skillCatalog) List() []SkillView {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]SkillView, 0, len(c.records))
	for _, record := range c.records {
		out = append(out, record.SkillView)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *skillCatalog) Load(name, reference string) (string, error) {
	c.mu.RLock()
	record, ok := c.records[name]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown skill %q", name)
	}
	target := "SKILL.md"
	if reference != "" {
		clean, err := validateRelativePath(reference)
		if err != nil || clean == "." || strings.EqualFold(clean, "SKILL.md") {
			return "", errors.New("invalid skill reference")
		}
		target = clean
	}
	if record.userRoot != "" {
		path := filepath.Join(record.userRoot, target)
		if !pathWithin(record.userRoot, path) {
			return "", errors.New("skill reference escapes its directory")
		}
		data, err := os.ReadFile(path)
		return string(data), err
	}
	data, err := fs.ReadFile(record.root, filepath.ToSlash(filepath.Join(record.dir, target)))
	return string(data), err
}

func loadSkillsFromFS(records map[string]skillRecord, root fs.FS, base, source, userRoot string) {
	entries, err := fs.ReadDir(root, base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.ToSlash(filepath.Join(base, entry.Name(), "SKILL.md"))
		data, readErr := fs.ReadFile(root, path)
		view := parseSkill(data, source)
		if readErr != nil {
			view = SkillView{Name: entry.Name(), Source: source, Error: readErr.Error()}
		}
		if view.Name == "" {
			view.Name = entry.Name()
		}
		records[view.Name] = skillRecord{
			SkillView: view,
			root:      root,
			dir:       filepath.ToSlash(filepath.Join(base, entry.Name())),
			userRoot:  userRoot,
		}
	}
}

func loadUserSkills(records map[string]skillRecord, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		data, readErr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		view := parseSkill(data, "user")
		if readErr != nil {
			view = SkillView{Name: entry.Name(), Source: "user", Error: readErr.Error()}
		}
		if view.Name == "" {
			view.Name = entry.Name()
		}
		records[view.Name] = skillRecord{SkillView: view, userRoot: dir}
	}
}

func parseSkill(data []byte, source string) SkillView {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return SkillView{Source: source, Error: "missing YAML frontmatter"}
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return SkillView{Source: source, Error: "unterminated YAML frontmatter"}
	}
	view := SkillView{Source: source}
	for _, line := range strings.Split(text[4:4+end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			view.Name = strings.Trim(strings.TrimSpace(value), `"'`)
		case "description":
			view.Description = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	if view.Name == "" || view.Description == "" {
		view.Error = "frontmatter requires name and description"
	}
	return view
}
