package agent

import (
	"encoding/json"
	"testing"
)

const sampleModelsDevDocument = `{
  "openai": {
    "id": "openai", "npm": "@ai-sdk/openai",
    "models": {
      "gpt-5.5": {
        "id": "gpt-5.5", "name": "GPT-5.5", "tool_call": true, "release_date": "2026-04-23",
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 1050000, "output": 128000},
        "reasoning_options": [{"type": "effort", "values": ["none", "high"]}]
      },
      "gpt-5.6": {
        "id": "gpt-5.6", "name": "GPT-5.6", "tool_call": true, "release_date": "2026-07-09",
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 1050000, "output": 128000},
        "experimental": {"modes": {"pro": {"provider": {"body": {"reasoning": {"mode": "pro"}}}}}}
      },
      "gpt-5.6-luna": {
        "id": "gpt-5.6-luna", "name": "GPT-5.6 Luna", "tool_call": true, "release_date": "2026-07-09",
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 1050000, "output": 128000},
        "experimental": {"modes": {
          "fast": {"provider": {"body": {"service_tier": "priority"}}},
          "pro": {"provider": {"body": {"reasoning": {"mode": "pro"}}}}
        }}
      },
      "gpt-5.4": {
        "id": "gpt-5.4", "name": "GPT-5.4", "tool_call": true, "release_date": "2026-03-05",
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 400000, "output": 400000},
        "reasoning_options": [{"type": "budget_tokens", "min": 1024}]
      },
      "gpt-5.1": {
        "id": "gpt-5.1", "name": "GPT-5.1", "tool_call": true, "release_date": "2025-11-13",
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 400000, "output": 128000}
      },
      "gpt-3.5-turbo": {
        "id": "gpt-3.5-turbo", "name": "GPT-3.5", "tool_call": false, "status": "deprecated",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 16000, "output": 4000}
      }
    }
  },
  "anthropic": {
    "id": "anthropic", "npm": "@ai-sdk/anthropic",
    "models": {
      "claude-sonnet-5": {
        "id": "claude-sonnet-5", "name": "Claude Sonnet 5", "tool_call": true, "release_date": "2026-01-05",
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 200000, "output": 64000},
        "reasoning_options": [{"type": "effort", "values": ["low", "high"]}]
      }
    }
  },
  "openrouter": {
    "id": "openrouter", "npm": "@openrouter/ai-sdk-provider",
    "models": {
      "vendor/model-a": {
        "id": "vendor/model-a", "name": "Model A", "tool_call": true, "release_date": "2026-02-02",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 100000, "output": 32000}
      }
    }
  },
  "vercel": {
    "id": "vercel", "npm": "@ai-sdk/gateway",
    "models": {
      "vendor/model-b": {
        "id": "vendor/model-b", "name": "Model B", "tool_call": true, "release_date": "2026-02-03",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 100000, "output": 32000}
      }
    }
  },
  "opencode": {
    "id": "opencode", "npm": "@ai-sdk/openai-compatible",
    "models": {
      "claude-sonnet-4-6": {
        "id": "claude-sonnet-4-6", "name": "Zen Claude", "tool_call": true, "release_date": "2026-02-17",
        "provider": {"npm": "@ai-sdk/anthropic"},
        "modalities": {"input": ["text", "image"], "output": ["text"]}, "limit": {"context": 1000000, "output": 64000}
      },
      "gemini-3.1-pro": {
        "id": "gemini-3.1-pro", "name": "Zen Gemini", "tool_call": true, "release_date": "2026-02-19",
        "provider": {"npm": "@ai-sdk/google"},
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1000000, "output": 64000}
      },
      "deepseek-v4-flash": {
        "id": "deepseek-v4-flash", "name": "Zen DeepSeek", "tool_call": true, "release_date": "2026-01-20",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 1000000, "output": 64000}
      }
    }
  },
  "opencode-go": {
    "id": "opencode-go", "npm": "@ai-sdk/openai-compatible",
    "models": {
      "kimi-k3": {
        "id": "kimi-k3", "name": "Kimi K3", "tool_call": true, "release_date": "2026-03-01",
        "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 256000, "output": 64000}
      }
    }
  },
  "google": {"id": "google", "npm": "@ai-sdk/google", "models": {}}
}`

func TestProjectCatalogKeepsSupportedProviders(t *testing.T) {
	t.Parallel()
	catalog, err := ProjectCatalog([]byte(sampleModelsDevDocument), "test", "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, 0, len(catalog.Providers))
	for _, provider := range catalog.Providers {
		ids = append(ids, provider.ID)
	}
	want := []string{providerOpenAI, providerAnthropic, providerOpenRouter, providerVercel, providerZen, providerGo}
	if len(ids) != len(want) {
		t.Fatalf("providers = %#v, want %#v", ids, want)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("providers = %#v, want %#v", ids, want)
		}
	}
}

func TestProjectCatalogFiltersAndRoutesModels(t *testing.T) {
	t.Parallel()
	catalog, err := ProjectCatalog([]byte(sampleModelsDevDocument), "test", "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	models := map[string]map[string]CatalogModel{}
	for _, provider := range catalog.Providers {
		index := map[string]CatalogModel{}
		for _, model := range provider.Models {
			index[model.ID] = model
		}
		models[provider.ID] = index
	}

	openai := models[providerOpenAI]
	if _, present := openai["gpt-3.5-turbo"]; present {
		t.Fatal("non-tool or deprecated model was shipped")
	}
	// models.dev marks the newer frontier models with fast and pro request modes; those modes are
	// variants of a served model, so plan eligibility still follows the id lists and the version.
	if denied := openai["gpt-5.6"]; denied.OAuth {
		t.Fatalf("denied model was marked available for the ChatGPT plan: %#v", denied)
	}
	if luna := openai["gpt-5.6-luna"]; !luna.OAuth {
		t.Fatalf("newer model with a pro mode was hidden from the ChatGPT plan: %#v", luna)
	}
	plan := openai["gpt-5.5"]
	if !plan.OAuth || plan.Protocol != protocolOpenAIResponses {
		t.Fatalf("gpt-5.5 = %#v", plan)
	}
	if !plan.SupportsImages || len(plan.Reasoning) != 2 {
		t.Fatalf("gpt-5.5 = %#v", plan)
	}
	if legacy := openai["gpt-5.1"]; legacy.OAuth {
		t.Fatalf("gpt-5.1 was marked available for the ChatGPT plan: %#v", legacy)
	}
	if clamped := openai["gpt-5.4"]; !clamped.OAuth || clamped.MaxOutputTokens >= clamped.ContextWindow {
		t.Fatalf("gpt-5.4 = %#v", clamped)
	}

	if openrouter := models[providerOpenRouter]["vendor/model-a"]; openrouter.Protocol != protocolOpenAICompatible {
		t.Fatalf("openrouter model = %#v", openrouter)
	}
	if vercel := models[providerVercel]["vendor/model-b"]; vercel.Protocol != protocolOpenAICompatible {
		t.Fatalf("vercel model = %#v", vercel)
	}

	zen := models[providerZen]
	if zen["claude-sonnet-4-6"].Protocol != protocolAnthropic {
		t.Fatalf("zen anthropic model = %#v", zen["claude-sonnet-4-6"])
	}
	if _, present := zen["gemini-3.1-pro"]; present {
		t.Fatal("unsupported Google route was shipped")
	}
	if zen["deepseek-v4-flash"].Protocol != protocolOpenAICompatible {
		t.Fatalf("zen compatible model = %#v", zen["deepseek-v4-flash"])
	}
}

func TestProjectCatalogOrdersNewestFirstAndKeepsOutputBelowContext(t *testing.T) {
	t.Parallel()
	catalog, err := ProjectCatalog([]byte(sampleModelsDevDocument), "test", "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	for _, provider := range catalog.Providers {
		for _, model := range provider.Models {
			if model.ContextWindow <= 0 {
				t.Fatalf("%s/%s has no context window", provider.ID, model.ID)
			}
			if model.MaxOutputTokens <= 0 || model.MaxOutputTokens >= model.ContextWindow {
				t.Fatalf(
					"%s/%s max output %d against context %d",
					provider.ID,
					model.ID,
					model.MaxOutputTokens,
					model.ContextWindow,
				)
			}
			if model.Protocol != protocolOpenAIResponses && model.Protocol != protocolAnthropic &&
				model.Protocol != protocolOpenAICompatible {
				t.Fatalf("%s/%s protocol %q", provider.ID, model.ID, model.Protocol)
			}
		}
	}

	var openai ProviderCatalog
	for _, provider := range catalog.Providers {
		if provider.ID == providerOpenAI {
			openai = provider
		}
	}
	if len(openai.Models) < 3 || openai.Models[0].ID != "gpt-5.6" ||
		openai.Models[1].ID != "gpt-5.6-luna" || openai.Models[2].ID != "gpt-5.5" {
		t.Fatalf("openai order = %#v", openai.Models)
	}
}

func TestProjectCatalogRejectsForeignDocument(t *testing.T) {
	t.Parallel()
	if _, err := ProjectCatalog([]byte(`{"google": {"id": "google", "models": {}}}`), "test", ""); err == nil {
		t.Fatal("ProjectCatalog accepted a document without shipped providers")
	}
}

func TestEmbeddedCatalogMatchesProviderSpecs(t *testing.T) {
	t.Parallel()
	snapshot := catalog()
	if len(snapshot.Providers) != len(providerSpecs) {
		t.Fatalf("catalog providers = %d, want %d", len(snapshot.Providers), len(providerSpecs))
	}
	for _, spec := range providerSpecs {
		models := catalogModelsFor(spec.ID)
		if len(models) == 0 {
			t.Fatalf("provider %s has no shipped models", spec.ID)
		}
		for _, model := range models {
			if resolved := resolveProtocol(spec.ID, model.ID); resolved != model.Protocol {
				t.Fatalf("%s/%s resolves to %q, want %q", spec.ID, model.ID, resolved, model.Protocol)
			}
			settings := applyCatalogDefaults(AgentSettingsView{
				Provider: spec.ID, Protocol: model.Protocol, Endpoint: spec.Endpoint, Model: model.ID,
				Reasoning: "auto",
			})
			if err := validateSettings(settings); err != nil {
				t.Fatalf("%s/%s defaults are invalid: %v", spec.ID, model.ID, err)
			}
		}
	}
}

func TestCatalogEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	snapshot, err := ProjectCatalog([]byte(sampleModelsDevDocument), "test", "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeCatalog(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCatalog(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Providers) != len(snapshot.Providers) {
		t.Fatalf("decoded providers = %d", len(decoded.Providers))
	}
	if _, err := DecodeCatalog([]byte(`{"providers": []}`)); err == nil {
		t.Fatal("DecodeCatalog accepted an empty projection")
	}
	if _, err := json.Marshal(decoded); err != nil {
		t.Fatal(err)
	}
}

func TestPlanDefaultModelMovesUnservedModel(t *testing.T) {
	t.Parallel()
	if model := planDefaultModel(providerOpenAI, "gpt-5.5"); model != "" {
		t.Fatalf("served model moved to %q", model)
	}
	model := planDefaultModel(providerOpenAI, "gpt-5.2")
	if model == "" {
		t.Fatal("unserved model was kept")
	}
	if served, ok := catalogModelFor(providerOpenAI, model); !ok || !served.OAuth {
		t.Fatalf("plan default %q is not served by the plan", model)
	}
}
