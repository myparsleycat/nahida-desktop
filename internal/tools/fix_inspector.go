package tools

import (
	"context"
	"strings"
	"sync"
)

type FixInspectionResult struct {
	NeedsFix      bool     `json:"needsFix"`
	Importer      string   `json:"importer"`
	ToolName      string   `json:"toolName"`
	Summary       string   `json:"summary"`
	Details       []string `json:"details"`
	AffectedFiles []string `json:"affectedFiles"`
	ActionTool    string   `json:"actionTool"`
}

type FixInspector interface {
	CanInspect(importer string) bool
	Inspect(ctx context.Context, modPath string) (*FixInspectionResult, error)
}

type FixInspectorRegistry struct {
	mu         sync.RWMutex
	inspectors []FixInspector
}

func NewFixInspectorRegistry() *FixInspectorRegistry {
	return &FixInspectorRegistry{
		inspectors: make([]FixInspector, 0),
	}
}

func (r *FixInspectorRegistry) Register(inspector FixInspector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inspectors = append(r.inspectors, inspector)
}

func (r *FixInspectorRegistry) Inspect(ctx context.Context, modPath, importer string) (*FixInspectionResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, inspector := range r.inspectors {
		if inspector.CanInspect(importer) {
			return inspector.Inspect(ctx, modPath)
		}
	}

	return &FixInspectionResult{
		NeedsFix: false,
		Importer: strings.ToUpper(importer),
	}, nil
}
