package agent

import (
	_ "embed"
	"strings"
)

//go:embed prompts/system.md
var systemPromptTemplate string

func renderSystemPrompt(
	scope, roots, summary, skills, actions, language string,
	supportsImages, patchText bool,
) string {
	return strings.NewReplacer(
		"{{scope}}", scope,
		"{{roots}}", roots,
		"{{summary}}", summary,
		"{{skills}}", skills,
		"{{actions}}", actions,
		"{{language}}", responseLanguage(language),
		"{{images}}", imageCapability(supportsImages),
		"{{editing}}", editingContract(patchText),
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

// editingContract describes the apply_patch variant the model was given, so the prompt never
// mentions fields its tool schema lacks.
func editingContract(patchText bool) string {
	if patchText {
		return "`apply_patch` is how files are changed: send one `patchText` envelope carrying every edit, " +
			"with an `*** Update File:` section per existing file and small `@@` chunks of context, `-`, and `+` " +
			"lines copied from the file. Name the enclosing section header after `@@` when a block repeats. " +
			"Never restate a whole file for a targeted edit. If a patch is rejected, correct the chunks the " +
			"error names and resend it. Existing files retain their encoding, BOM, and newline style."
	}
	return "An `update` in `apply_patch` is how an existing file is changed: supply `oldString` and `newString` " +
		"so `oldString` matches exactly one place unless `replaceAll` is true, and include a unique nearby " +
		"section header when a snippet repeats. Put every edit to a file in one `apply_patch` call: several " +
		"`update` operations may target the same path and apply in order, each to the result of the previous " +
		"one. Never use `write` for a targeted edit. If a call is rejected, correct the operations the error " +
		"names and resend it instead of resending the whole file. A `write` supplies complete replacement " +
		"text only when the entire file must be replaced; `expectedContent` is the complete decoded text " +
		"observed earlier, and existing files retain their encoding, BOM, and newline style."
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
