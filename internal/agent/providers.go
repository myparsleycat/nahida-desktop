package agent

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// Built-in providers. Each spec fixes the endpoint and the default wire protocol, so the user
// only chooses a model; providerCustom keeps every field editable.
const (
	providerCustom     = "custom"
	providerOpenAI     = "openai"
	providerAnthropic  = "anthropic"
	providerOpenRouter = "openrouter"
	providerVercel     = "vercel"
	providerZen        = "opencode"
	providerGo         = "opencode-go"
)

const (
	protocolOpenAIResponses  = "openai-responses"
	protocolOpenAICompatible = "openai-compatible"
	protocolAnthropic        = "anthropic"
)

//go:embed providers/catalog.json
var embeddedCatalogJSON []byte

// ModelCatalog is the shipped model list for the built-in providers. It is a projection of
// https://models.dev/api.json produced by internal/agent/cataloggen.
type ModelCatalog struct {
	Source    string            `json:"source"`
	UpdatedAt string            `json:"updatedAt"`
	Providers []ProviderCatalog `json:"providers"`
}

// ProviderCatalog groups the models one provider serves.
type ProviderCatalog struct {
	ID     string         `json:"id"`
	Models []CatalogModel `json:"models"`
}

// CatalogModel describes one selectable model: the wire protocol it needs, its limits, and the
// reasoning efforts it accepts.
type CatalogModel struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Protocol        string   `json:"protocol"`
	ContextWindow   int      `json:"contextWindow"`
	MaxOutputTokens int      `json:"maxOutputTokens"`
	SupportsImages  bool     `json:"supportsImages"`
	Reasoning       []string `json:"reasoning,omitempty"`
	OAuth           bool     `json:"oauth"`
	Status          string   `json:"status,omitempty"`
}

// providerSpec fixes the endpoint and default wire protocol of a built-in provider. Headers are
// applied to provider requests and never stored in user settings.
type providerSpec struct {
	ID              string
	Name            string
	Endpoint        string
	DefaultProtocol string
	OAuth           bool
	Headers         []AgentHeaderView
}

var providerSpecs = []providerSpec{
	{
		ID: providerOpenAI, Name: "OpenAI", Endpoint: "https://api.openai.com/v1",
		DefaultProtocol: protocolOpenAIResponses, OAuth: true,
	},
	{
		ID: providerAnthropic, Name: "Anthropic", Endpoint: "https://api.anthropic.com/v1",
		DefaultProtocol: protocolAnthropic,
	},
	{
		ID: providerOpenRouter, Name: "OpenRouter", Endpoint: "https://openrouter.ai/api/v1",
		DefaultProtocol: protocolOpenAICompatible,
		Headers: []AgentHeaderView{
			{Name: "HTTP-Referer", Value: "https://nahida.live/"},
			{Name: "X-Title", Value: "Nahida Desktop"},
		},
	},
	{
		ID: providerVercel, Name: "Vercel AI Gateway", Endpoint: "https://ai-gateway.vercel.sh/v1",
		DefaultProtocol: protocolOpenAICompatible,
	},
	{
		ID: providerZen, Name: "OpenCode Zen", Endpoint: "https://opencode.ai/zen/v1",
		DefaultProtocol: protocolOpenAICompatible,
	},
	{
		ID: providerGo, Name: "OpenCode Go", Endpoint: "https://opencode.ai/zen/go/v1",
		DefaultProtocol: protocolOpenAICompatible,
		Headers: []AgentHeaderView{
			{Name: "x-opencode-session", Value: "{{ session_id }}"},
		},
	},
}

func providerSpecFor(id string) (providerSpec, bool) {
	for _, spec := range providerSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return providerSpec{}, false
}

// isBuiltinProvider reports whether the id selects a shipped provider rather than a custom one.
func isBuiltinProvider(id string) bool {
	_, ok := providerSpecFor(id)
	return ok
}

var (
	catalogOnce    sync.Once
	catalogDefault *ModelCatalog
	catalogCurrent atomic.Pointer[ModelCatalog]
)

// DecodeCatalog parses a catalog document, rejecting an empty projection.
func DecodeCatalog(raw []byte) (*ModelCatalog, error) {
	parsed := &ModelCatalog{}
	if err := json.Unmarshal(raw, parsed); err != nil {
		return nil, err
	}
	if len(parsed.Providers) == 0 {
		return nil, errors.New("catalog contains no providers")
	}
	return parsed, nil
}

// EncodeCatalog renders a catalog for storage; cataloggen writes the same shape it ships.
func EncodeCatalog(snapshot *ModelCatalog) ([]byte, error) {
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// catalog returns the effective catalog: the refreshed snapshot when one has been loaded, and the
// embedded projection otherwise.
func catalog() *ModelCatalog {
	if current := catalogCurrent.Load(); current != nil {
		return current
	}
	catalogOnce.Do(func() {
		parsed, err := DecodeCatalog(embeddedCatalogJSON)
		if err != nil {
			parsed = &ModelCatalog{}
		}
		catalogDefault = parsed
	})
	return catalogDefault
}

// useCatalog installs a refreshed catalog for the rest of the process lifetime.
func useCatalog(snapshot *ModelCatalog) {
	if snapshot == nil || len(snapshot.Providers) == 0 {
		return
	}
	catalogCurrent.Store(snapshot)
}

func catalogModelsFor(providerID string) []CatalogModel {
	for _, provider := range catalog().Providers {
		if provider.ID == providerID {
			return provider.Models
		}
	}
	return nil
}

func catalogModelFor(providerID, modelID string) (CatalogModel, bool) {
	for _, model := range catalogModelsFor(providerID) {
		if model.ID == modelID {
			return model, true
		}
	}
	return CatalogModel{}, false
}

// planDefaultModel returns the model to use after a plan login: empty when the plan serves the
// stored model, otherwise the newest model it does serve. The catalog is ordered newest first.
func planDefaultModel(providerID, modelID string) string {
	fallback := ""
	for _, model := range catalogModelsFor(providerID) {
		if !model.OAuth {
			continue
		}
		if fallback == "" {
			fallback = model.ID
		}
		if model.ID == modelID {
			return ""
		}
	}
	return fallback
}

// resolveProtocol picks the wire protocol for a model of a built-in provider. Models outside the
// catalog fall back to the provider default so a hand-typed id still has a usable route.
func resolveProtocol(providerID, modelID string) string {
	spec, ok := providerSpecFor(providerID)
	if !ok {
		return ""
	}
	if model, found := catalogModelFor(providerID, modelID); found && model.Protocol != "" {
		return model.Protocol
	}
	return spec.DefaultProtocol
}
