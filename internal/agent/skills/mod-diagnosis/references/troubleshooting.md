# Symptom-driven troubleshooting

Use the narrowest row that matches the user's report, then verify it against the actual files. Several symptoms share causes, so these are priorities rather than guaranteed diagnoses.

## Nothing loads

Ask the user to confirm manually that the correct importer is running, the mod is under its active Mods directory, the target object is visible, and `F10` was pressed after changes. In files, check wrapper-directory mistakes, disabled INIs, syntax errors, unresolved includes, and missing resource filenames.

If every mod fails, the issue is more likely setup-wide than a defect in the selected mod. ShaderCache or ShaderFixes can contribute, but do not erase either directory. Suggest temporarily moving or disabling a small, backed-up set only as a user-operated isolation test.

## Yellow errors or one mod fails to appear

Prioritize the exact error text and referenced INI line. Common causes include:

- filename, extension, or relative-path mismatch;
- malformed INI syntax or an inline comment after a command;
- a referenced Resource, CommandList, or include that does not exist;
- DDS format/dimensions incompatible with the original resource; and
- missing vertex groups or an export that omitted expected model data.

Do not assume textures must always be powers of two; compare with the dumped/original texture because some valid game assets use unusual dimensions.

## Orange conflict warnings

Multiple active overrides are usually matching the same hash. Search all visible INIs for that hash and determine whether two installed mods target the same object, the same mod was duplicated, or one package contains intentional layered overrides. Do not silence the warning with `allow_duplicate_hash` or `match_priority` unless the desired winner and runtime behavior are understood.

## Crash at startup or on a scene

Check whether the crash began after a game/importer update or adding one mod. Use binary isolation across Mods; if the crash remains with Mods disabled, isolate ShaderFixes separately. Once narrowed to one package, inspect version-specific hashes, shaders, includes, buffer strides, draw counts, and executable dependencies. Recommend updates only as user-performed research; do not access or download them.

## Partial render, collapse, or severe deformation

If it occurs only on the first load, have the user reload the object by leaving the scene or restarting the game before changing files. If persistent:

- confirm vertex-group numbers, order, gaps, and weights against the original part;
- verify position/blend buffers are bound to the intended slots;
- verify buffer byte lengths divide by the declared stride;
- check draw or indexed-draw counts and first-index ranges; and
- distinguish one broken part from a whole-model layout mismatch.

Slight deformation more often suggests weights; chaotic geometry more often suggests group ordering, stride/layout, wrong buffers, or stale importer hashes.

## Wrong orientation or scale

The replacement and importer source may use different coordinate spaces. Check applied rotation/scale and the importer's expected export orientation. When a capable Blender MCP is enabled, inspect and correct the relevant objects through it while preserving unrelated scene state; otherwise provide the Blender steps as manual instructions.

## Wrong, black, bright, or missing textures

Trace the override slot to the Resource and file, then compare it with the original/dump:

- DDS format, dimensions, mip behavior, and linear versus sRGB variant;
- alpha and packed-channel meaning;
- normal direction and tangent expectations;
- primary UV naming such as `TEXCOORD.xy` and required additional UV layers;
- texture-coordinate buffers and slot ordering; and
- conditional branches that bind different textures.

A very bright/glowing result can be an alpha/emission problem, but packed channels vary by game. An opaque mesh can require shader/blend support such as an already installed TexFX setup; describe that dependency without obtaining it.

### GIMI green parts and ORFix/NNFix

Use the parent skill's direct-fix rule before broad texture diagnosis. A green non-face part is not limited to the historical case of an incompatible `ORFix` call: it can also be a no-normal render path that is missing the canonical `run = CommandList\global\ORFix\NNFix` call.

When an affected active `TextureOverride` binds `ps-t0` to Diffuse and `ps-t1` to LightMap, has no active normal-map binding, and lacks an active NNFix call, add the NNFix call immediately after the texture assignments. Matching `Head` and `Body` sections are eligible; do not automatically apply the rule to `Face`. The command is global and need not be defined in the mod folder. After an authorized edit, re-read the changed sections and stop unless verification fails or the user reports that the symptom remains.

## Hashes changed after an update

Use a compatible built-in fixer dry run first. Otherwise compare the same game/importer version and same object/resource type. Keep shader hashes, texture hashes, vertex/index buffer hashes, and draw ranges distinct. Never paste an example hash from documentation or a different version.

Private-server or older-game builds may need a reverse/downgrade mapping rather than the newest public-game fix. Report this possibility; do not search for or run a reverse-fix utility.

## Game-specific clues

- Genshin Impact: explosive geometry can be related to Dynamic Character Resolution being enabled, as well as ordinary buffer/hash problems. Ask the user to check the game setting manually before editing an otherwise sound mod.
- Genshin Impact: wrong reflection/outline color and green/multicolored skin have multiple ORFix/NNFix cases; follow the parent skill's exact fast path before broader investigation.
- Zenless Zone Zero: disappearance at distance can be normal LOD switching. A mod may cover only the near model. Low-VRAM texture selection can also expose resolution-specific hashes that a mod does not cover.
- Honkai: Star Rail: importer/game updates may require structural buffer changes in addition to hash replacement. Never synthesize position/blend conversion fields or vertex counts without inspecting the actual buffers and current importer convention.

## External assistance boundary

The source material mentions launchers, Blender, importer development builds, TexFX, GUI Collect, community sites, Discord, and third-party fix scripts. Nahida may operate Blender only through a capable enabled Blender MCP. It must not visit, download, install, execute, or vouch for the other resources, and a Blender MCP does not authorize installing Blender or add-ons. Prefer registered tools and inspected local evidence.

## Provenance

Adapted from the locally supplied `leotorrez/modding` troubleshooting guide at revision `344a7cbfc1a03693205dfa8c01da312a68b8c841` (GPL-3.0), with version-specific prescriptions generalized to avoid treating old hashes or third-party downloads as current facts. No external access is required.
