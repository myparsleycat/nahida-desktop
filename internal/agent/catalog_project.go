package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// shippedModelLimits caps the picker size for catalog providers that list hundreds of models.
// Providers without an entry keep every model they serve.
var shippedModelLimits = map[string]int{
	providerOpenRouter: 50,
	providerVercel:     50,
}

// protocolByPackage maps the AI SDK package a models.dev model declares to the wire protocol the
// agent implements. Packages without a mapping (Google, Bedrock, ...) are not shipped.
var protocolByPackage = map[string]string{
	"@ai-sdk/openai":            protocolOpenAIResponses,
	"@ai-sdk/anthropic":         protocolAnthropic,
	"@ai-sdk/openai-compatible": protocolOpenAICompatible,
}

// chatGPTPlanAllowed and chatGPTPlanDenied mirror the model set opencode exposes for ChatGPT
// Pro/Plus credentials; every other id must be newer than gpt-5.4.
var (
	chatGPTPlanAllowed = map[string]bool{
		"gpt-5.5":             true,
		"gpt-5.3-codex-spark": true,
		"gpt-5.4":             true,
		"gpt-5.4-mini":        true,
	}
	chatGPTPlanDenied  = map[string]bool{"gpt-5.5-pro": true, "gpt-5.6": true}
	chatGPTPlanVersion = regexp.MustCompile(`^gpt-(\d+)(?:\.(\d+))?`)
)

type sourceProvider struct {
	ID     string                 `json:"id"`
	NPM    string                 `json:"npm"`
	Models map[string]sourceModel `json:"models"`
}

type sourceModel struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Reasoning        bool   `json:"reasoning"`
	ReasoningOptions []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	ToolCall    bool   `json:"tool_call"`
	ReleaseDate string `json:"release_date"`
	Modalities  struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Status   string `json:"status"`
	Provider struct {
		NPM string `json:"npm"`
	} `json:"provider"`
}

// ProjectCatalog turns a models.dev api.json document into the catalog the agent ships: the
// supported providers, their tool-capable text models, and the protocol each one needs.
func ProjectCatalog(raw []byte, source string, updatedAt string) (*ModelCatalog, error) {
	var document map[string]sourceProvider
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode models.dev catalog: %w", err)
	}

	catalog := &ModelCatalog{Source: source, UpdatedAt: updatedAt}
	for _, spec := range providerSpecs {
		provider, present := document[spec.ID]
		if !present {
			continue
		}
		models := projectProviderModels(spec, provider)
		if len(models) == 0 {
			continue
		}
		catalog.Providers = append(catalog.Providers, ProviderCatalog{ID: spec.ID, Models: models})
	}
	if len(catalog.Providers) == 0 {
		return nil, fmt.Errorf("models.dev document contains none of the shipped providers")
	}
	return catalog, nil
}

func projectProviderModels(spec providerSpec, provider sourceProvider) []CatalogModel {
	models := make([]CatalogModel, 0, len(provider.Models))
	for _, model := range provider.Models {
		if projected, ok := projectModel(spec, provider.NPM, model); ok {
			models = append(models, projected)
		}
	}

	sort.Slice(models, func(left, right int) bool {
		leftDate, rightDate := modelReleaseDate(
			provider.Models[models[left].ID],
		), modelReleaseDate(
			provider.Models[models[right].ID],
		)
		if leftDate != rightDate {
			return leftDate > rightDate
		}
		return models[left].ID < models[right].ID
	})
	if limit := shippedModelLimits[spec.ID]; limit > 0 && len(models) > limit {
		models = models[:limit]
	}
	return models
}

func projectModel(spec providerSpec, providerPackage string, model sourceModel) (CatalogModel, bool) {
	if !model.ToolCall || model.Status == "deprecated" || !contains(model.Modalities.Output, "text") {
		return CatalogModel{}, false
	}
	if model.Limit.Context <= 0 {
		return CatalogModel{}, false
	}

	// A model that opts into its own SDK must map to a protocol the agent implements; gateways
	// that front many vendors (OpenRouter, Vercel AI Gateway) fall back to the provider default.
	protocol := spec.DefaultProtocol
	switch mapped, ok := protocolByPackage[providerPackage]; {
	case model.Provider.NPM != "":
		protocol, ok = protocolByPackage[model.Provider.NPM]
		if !ok {
			return CatalogModel{}, false
		}
	case ok:
		protocol = mapped
	}

	projected := CatalogModel{
		ID:              model.ID,
		Name:            displayModelName(model),
		Protocol:        protocol,
		ContextWindow:   model.Limit.Context,
		MaxOutputTokens: model.Limit.Output,
		SupportsImages:  contains(model.Modalities.Input, "image"),
		Reasoning:       reasoningEfforts(model),
		OAuth:           true,
	}
	if spec.ID == providerOpenAI {
		projected.OAuth = chatGPTPlanModel(model)
	}
	// Settings require the output limit to stay below the context window; models.dev reports
	// equal values for some gateway models.
	if projected.MaxOutputTokens <= 0 || projected.MaxOutputTokens >= projected.ContextWindow {
		projected.MaxOutputTokens = projected.ContextWindow * 3 / 4
	}
	return projected, true
}

func displayModelName(model sourceModel) string {
	if strings.TrimSpace(model.Name) != "" {
		return model.Name
	}
	return model.ID
}

func modelReleaseDate(model sourceModel) string {
	if strings.TrimSpace(model.ReleaseDate) != "" {
		return model.ReleaseDate
	}
	return "0000-00-00"
}

// reasoningEfforts keeps the effort values the agent can send; Anthropic's token budgets and
// other option types are not exposed.
func reasoningEfforts(model sourceModel) []string {
	efforts := make([]string, 0, len(model.ReasoningOptions))
	for _, option := range model.ReasoningOptions {
		if option.Type != "effort" {
			continue
		}
		for _, value := range option.Values {
			if value != "" && !contains(efforts, value) {
				efforts = append(efforts, value)
			}
		}
	}
	return efforts
}

// chatGPTPlanModel reports whether opencode exposes the model for ChatGPT Pro/Plus credentials:
// ids must be allow-listed or newer than gpt-5.4, minus the denied entries. The fast and pro modes
// models.dev publishes for these models are request variants, so they never hide a model.
func chatGPTPlanModel(model sourceModel) bool {
	if chatGPTPlanAllowed[model.ID] {
		return true
	}
	if chatGPTPlanDenied[model.ID] {
		return false
	}
	match := chatGPTPlanVersion.FindStringSubmatch(model.ID)
	if match == nil {
		return false
	}
	major, _ := strconv.Atoi(match[1])
	minor := 0
	if match[2] != "" {
		minor, _ = strconv.Atoi(match[2])
	}
	return major > 5 || (major == 5 && minor > 4)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
