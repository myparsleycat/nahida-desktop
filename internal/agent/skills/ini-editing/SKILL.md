---
name: ini-editing
description: Analyze and safely edit standard or XXMI/3DMigoto INI files while preserving syntax, references, encoding, and layout.
---

# INI editing

- Match inspection depth to the edit. For an explicit modification request with a known, deterministic local pattern, inspect the affected section, check that the target command is not already active, apply the smallest patch, re-read the changed region, and stop.
- Read the relevant section, nearby comments, and referenced sections before changing a value.
- Preserve encoding, BOM, newline style, comments, section ordering, indentation, and unknown keys.
- Use patch preconditions when replacing content observed earlier in the turn.
- Avoid broad rewrites when a focused edit is sufficient.
- Trace resource and command-list references in both directions only when the change depends on their definitions or compatibility. A syntactically valid change can still leave a missing filename, dead resource, incompatible stride, or broken render path, but do not expand into unrelated resources after a specialized skill's exact rule has sufficient evidence.
- Do not normalize an XXMI/3DMigoto INI with a generic INI library. Its repeated commands, command lists, conditions, variables, and modifiers are not ordinary key/value configuration.
- Never copy hashes, strides, draw counts, or formats from another mod without compatibility evidence.

Read [references/3dmigoto-syntax.md](references/3dmigoto-syntax.md) for XXMI/3DMigoto INI structure, overrides, resources, command lists, variables, and validation checks.

## Detailed imported references

Load only the detailed reference needed for the current file and only when this entrypoint, the summarized syntax reference, and local evidence are insufficient. A specialized skill's exact rule takes precedence over these imported references. Treat examples as source-era behavior rather than guaranteed support in every importer.

- Read [references/source-ini-language.md](references/source-ini-language.md) for the supplied documentation's full treatment of sections, namespace, command lists, modifiers, constants, variables, operators, conditions, and Present.
- Read [references/source-overrides-and-resources.md](references/source-overrides-and-resources.md) for common and advanced override matching properties plus Resource fields.
- Read [references/source-keys-and-custom-shaders.md](references/source-keys-and-custom-shaders.md) for key types, combinations, transitions, presets, render state, blending, and CustomShader properties.
- Read [references/source-shader-overrides.md](references/source-shader-overrides.md) for the source's larger ShaderOverride property catalogue and examples.
