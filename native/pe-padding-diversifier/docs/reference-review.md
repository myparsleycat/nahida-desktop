# Reference Review

This project was designed after inspecting the requested reference repositories:

- `noahware/binprotect`
- `sondt99/ObfuGuard`

The cloned repositories are local technical references only and are ignored by Git.

## Licenses

`noahware/binprotect` does not contain a top-level license file in the inspected clone. Some bundled `raw_pdb` files carry a BSD-2-Clause license, but the repository as a whole is treated as all-rights-reserved. No code is copied or translated from it.

`sondt99/ObfuGuard` is MIT licensed. This implementation still avoids a line-by-line port and uses only high-level concepts, because the goal here is a pure Rust, layout-preserving padding transformer rather than a C++ trampoline or full obfuscator.

## Concepts Adopted

From `binprotect`:

- Treat RVAs and file offsets as distinct concepts and reject ambiguous conversions.
- Build code reachability from explicit code roots such as entry points, exports, runtime functions, relocations, and branch targets.
- Track direct branches, fallthrough, RIP-relative references, and jump-table-like references as safety signals.
- Treat exception/unwind metadata and relocation metadata as protected.
- Prefer conservative false negatives over rewriting references.

From `ObfuGuard`:

- Validate PE64 loading before attempting transformation.
- Decode instruction boundaries before changing code-section bytes.
- Treat `CALL`/`JMP` rel32 and RIP-relative operands as sensitive references.
- Use deterministic randomization instead of process-time randomness.
- Verify output by reparsing and comparing structural PE metadata.

## Design Plan

The first implementation intentionally avoids function relocation, new sections, section resizing, instruction rewriting, import/export changes, unwind edits, and loader behavior changes.

The Rust core exposes a library API that accepts bytes and returns transformed bytes plus a structured report. The CLI only handles filesystem input/output.

The implementation is separated into:

- PE parsing and address conversion.
- Metadata directory and protected-range extraction.
- Instruction decoding and conservative reference collection.
- Padding discovery.
- Candidate safety validation.
- Deterministic in-place patch planning.
- Output mutation and post-transform validation.
- CLI and JSON reporting.

The transformer only changes bytes inside approved padding candidates. It never changes file length, section layout, data-directory layout, entry point, imports, exports, relocations, exception directory, TLS metadata, or load-config metadata.
