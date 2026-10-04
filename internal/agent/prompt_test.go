package agent

import (
	"strings"
	"testing"
)

func TestRenderSystemPromptReplacesRuntimeContext(t *testing.T) {
	t.Parallel()
	prompt := renderSystemPrompt(
		"mod",
		`[{"id":"root"}]`,
		"durable summary",
		`[{"name":"mod-diagnosis"}]`,
		`[{"id":"mod.update_toggle_key","description":"Update a mod toggle key.","risk":"confirm"}]`,
		"ko",
		true,
		false,
	)
	for _, expected := range []string{
		"Respond in Korean (ko), the language selected in Nahida Desktop",
		"Current scope: mod",
		"Image input: enabled. Screenshots from tools and images attached to user messages arrive as pictures",
		`Sandbox roots: [{"id":"root"}]`,
		"Durable conversation summary: durable summary",
		"Skills catalog (call `load_skill` for full instructions): [{\"name\":\"mod-diagnosis\"}]",
		"An explicit request to fix, modify, or update files authorizes focused edits",
		"make the smallest reversible edit, re-read the changed region to verify it, and stop",
		"Do not delay that edit with generic diagnosis, broad tool discovery, unrelated file inspection",
		"Prefer a purpose-built desktop action over generic file tools",
		"query `list_desktop_actions` once with the exact action ID or a narrow phrase",
		"Never create or modify probe files merely to discover tool semantics",
		"An `update` in `apply_patch` is how an existing file is changed",
		"Never use `write` for a targeted edit",
		"correct the operations the error names and resend it instead of resending the whole file",
		"Every approval request must directly advance the user's requested outcome",
		"Use locally available application state before asking the user to retype a configured path",
		"A delivered key or launched process is not proof of the expected game state",
		`Desktop action index (ID, description, and risk only): [{"id":"mod.update_toggle_key"`,
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("rendered system prompt is missing %q", expected)
		}
	}
	if strings.Contains(prompt, "{{") {
		t.Fatalf("rendered system prompt contains an unresolved placeholder: %q", prompt)
	}
}

// A model that edits through the patch envelope must not be told about JSON operation fields.
func TestRenderSystemPromptDescribesPatchTextEditing(t *testing.T) {
	t.Parallel()
	prompt := renderSystemPrompt("mod", "[]", "", "[]", "[]", "en", false, true)
	if !strings.Contains(prompt, "send one `patchText` envelope carrying every edit") {
		t.Fatalf("rendered system prompt is missing the patch text contract: %q", prompt)
	}
	for _, unexpected := range []string{"`oldString`", "`expectedContent`", "{{"} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("rendered system prompt mentions %q", unexpected)
		}
	}
}

func TestResponseLanguageFallsBackToEnglish(t *testing.T) {
	t.Parallel()

	for _, language := range []string{"", "unsupported"} {
		if got := responseLanguage(language); got != "English (en)" {
			t.Fatalf("responseLanguage(%q) = %q, want English (en)", language, got)
		}
	}
}
