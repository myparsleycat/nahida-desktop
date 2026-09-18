package agent

import (
	_ "embed"
	"strings"
)

//go:embed prompts/system.md
var systemPromptTemplate string

func renderSystemPrompt(scope, roots, summary, skills, actions, language string, supportsImages bool) string {
	return strings.NewReplacer(
		"{{scope}}", scope,
		"{{roots}}", roots,
		"{{summary}}", summary,
		"{{skills}}", skills,
		"{{actions}}", actions,
		"{{language}}", responseLanguage(language),
		"{{images}}", imageCapability(supportsImages),
	).Replace(systemPromptTemplate)
}

// imageCapability states whether the configured model receives pictures, so it never invents the
// contents of an image it cannot see.
func imageCapability(supportsImages bool) string {
	if supportsImages {
		return "enabled. Screenshots from tools and images attached to user messages arrive as " +
			"pictures; inspect them directly"
	}
	return "disabled. You cannot see image content, and image-producing tools only report " +
		"metadata; never describe or guess what an image shows"
}

func responseLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "ko":
		return "Korean (ko)"
	case "ja":
		return "Japanese (ja)"
	case "zh":
		return "Simplified Chinese (zh)"
	default:
		return "English (en)"
	}
}
