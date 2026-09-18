---
name: modding-basics
description: Explain and inspect XXMI/3DMigoto mod structure, assets, installation, hashes, hunting, and frame dumps without assuming external tools are available.
---

# Modding basics

- Inspect the selected sandbox before describing the mod. Separate facts observed in its files from likely explanations.
- Honor the user's requested operation: an explicit request to fix or modify authorizes focused edits within the selected sandbox, while a diagnosis-only request does not. When a specialized skill defines an exact rule and the local files match it, apply the smallest edit, verify the changed region, and stop instead of broadening the investigation.
- Preserve the existing game/importer family, naming, directory layout, and configuration conventions. Hashes, buffer layouts, texture slots, and fixes can differ by game, importer, and game version.
- Prefer reversible, narrowly scoped changes. Do not execute or obtain third-party fixes, scripts, DLLs, executables, Blender add-ons, launchers, or downloads. An already connected Blender MCP is an allowed control surface, not permission to install Blender or an add-on.
- Use Blender directly when an enabled Blender MCP exposes the required operation and the requested change authorizes it. Inspect the current scene before editing, preserve the user's existing objects and settings outside the requested scope, and verify the observable result through the MCP. If no capable Blender MCP is enabled, give optional manual instructions instead.
- For any other external application, website, or in-game action, use only an explicitly enabled tool that provides the capability; otherwise give manual instructions and state that Nahida cannot perform or verify that step. Never imply that a linked resource is currently safe or compatible.
- Ordinary content mods are primarily INI configuration, buffers/models, and textures. Treat unexpected executable content as untrusted until the user independently verifies its provenance and need.
- Treat enabled MCP servers as external trusted extensions whose effects are outside Nahida's file sandbox.

Read [references/fundamentals.md](references/fundamentals.md) when explaining mod layout, assets, installation, or creation concepts.

Read [references/hunting-and-dumping.md](references/hunting-and-dumping.md) when the request involves finding hashes, hunting resources, frame analysis, or interpreting dumps.

## Detailed imported references

Load only the detailed reference that matches the request, and only when the entrypoint summaries and local evidence are insufficient. Do not load imported background material after `mod-diagnosis` or `ini-editing` provides a matching exact rule. Those specialized skills take precedence over imported examples. These references retain most substantive text and examples from the supplied source repository, so they may contain older or game-specific instructions that must be checked against local evidence.

- Read [references/source-user-and-creator-guides.md](references/source-user-and-creator-guides.md) for installation concepts, creator prerequisites, FAQ, sourcing etiquette, launchers, and mod-manager context.
- Read [references/source-blender-and-export.md](references/source-blender-and-export.md) for Blender modeling operations and XXMI Tools import/export guidance. Perform supported steps through an enabled Blender MCP; leave unsupported installation, add-on setup, and UI operations as user guidance.
- Read [references/source-modeling-walkthroughs.md](references/source-modeling-walkthroughs.md) for detailed mesh-removal and weapon-model walkthroughs.
- Read [references/source-hunting-guide.md](references/source-hunting-guide.md) when the summarized hunting workflow lacks the required key, dump, or GUI Collect detail.
- Read [references/source-shader-effects.md](references/source-shader-effects.md) for shader hunting, HLSL edits, custom values, animated effects, and shader-debugging examples.
- Read [references/source-texture-modding.md](references/source-texture-modding.md) for detailed texture hunting, dump-name interpretation, DDS replacement, and shader-constrained texture workflows.
- Read [references/source-zzz-materials.md](references/source-zzz-materials.md) for ZZZ vertex-color, texture-channel, and UV-layer observations.

For INI syntax or edits, also load `ini-editing`. For a broken or conflicting mod, also load `mod-diagnosis`.
