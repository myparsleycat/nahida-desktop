# RabbitFX removal playbook

This reference expands the reasoning rules used by the main skill.

## What RabbitFX is doing in the common texture-binding pattern

In the common pattern, the mod writes local resource references into a RabbitFX namespace and then executes an external command list such as `Commandlist\RabbitFX\SetTextures`. That command list is the missing dependency: it translates semantic texture channels into pixel-shader resource slots and may also protect/restore state.

Removing the dependency therefore requires reproducing the observable state change locally.

## Evidence hierarchy for slot mapping

### Strong evidence

A sibling INI directly binds semantically equivalent resources and clearly saves/restores the same slots. Example:

```ini
ResourceTempT0 = ref ps-t0
ResourceTempT1 = ref ps-t1
ResourceTempT2 = ref ps-t2
ResourceTempT3 = ref ps-t3

ps-t0 = ref Resource_Texture_normal
ps-t1 = ref Resource_Texture_light
ps-t2 = ref Resource_Texture_material
ps-t3 = ref Resource_Texture_main
```

This is strong evidence for the mapping in that package.

### Medium evidence

Several components in the same INI use direct bindings with consistent semantic naming, or a generator family emits an identical ordering in multiple non-RabbitFX blocks.

### Weak evidence

Filename suffixes (`D`, `N`, `L`, `M`) or community conventions without corroboration from the package. These are useful clues but should not be the sole basis for an irreversible rewrite.

## Scope the lifetime of a binding

A RabbitFX group often establishes state for a sequence of draw calls until another group replaces it. Preserve this lifetime exactly.

Incorrect:

```ini
; one binding copied to the top of the section
ps-t0 = ref ResourceA_N
...
; draw calls that originally used ResourceB now incorrectly still use A
```

Correct:

```ini
ps-t0 = ref ResourceA_N
...draws using A...
ps-t0 = ref ResourceB_N
...draws using B...
```

## Save/restore strategy

Direct `ps-tN` assignment can leak state into later draws. Save before the first direct replacement in an affected region and restore after the final affected draw.

Prefer one save and one restore per contiguous affected override section, not one pair per individual draw, unless the surrounding control flow requires narrower scope.

If a conditional branch can bypass the restore after a save, restructure the code so every save path reaches a restore path.

## Multi-section mods

Treat each `[TextureOverride...]` or equivalent draw section independently. Do not save in one section and restore in another; execution ordering may differ from file ordering.

## Effect calls

Not every RabbitFX call is a texture setter. An effect call hands an input to shader logic inside RabbitFX and is usually paired with a second call that resets it after the covered draws:

```ini
Resource\RabbitFX\FXMap = ref ResourceTextureChestFX
run = CommandList\RabbitFX\Run
if $draw_component_3_006
    drawindexed = 4998, 147354, 0
endif
Resource\RabbitFX\FXMap = ref ResourceTextureChestFXoff
run = CommandList\RabbitFX\Run
```

Without the RabbitFX implementation this effect cannot be rebuilt from the mod alone, and no slot mapping proves what it does. Retire it: comment out both assignment/run pairs and keep the draw.

```ini
; RabbitFX effect retired: FXMap has no local equivalent
; Resource\RabbitFX\FXMap = ref ResourceTextureChestFX
; run = CommandList\RabbitFX\Run
if $draw_component_3_006
    drawindexed = 4998, 147354, 0
endif
; Resource\RabbitFX\FXMap = ref ResourceTextureChestFXoff
; run = CommandList\RabbitFX\Run
```

The mesh still renders with whatever texture state is active at that point; the overlay or animation the effect produced is gone, and the report must say so. The setter groups in the same file are converted as usual. An effect call is a reason to ask for the RabbitFX files only when no local draw sits between the call and its reset, because the external command list may then issue the draw itself.

## Resource-section checks

When a direct binding says:

```ini
ps-t3 = ref ResourceTextureBodyD
```

confirm that a matching section exists when the resource is local:

```ini
[ResourceTextureBodyD]
filename = Textures/BodyD.dds
```

External framework resources may intentionally lack local definitions, but those should be understood before preserving or removing them.

## Common failure modes

1. **Delete-only conversion**: removes RabbitFX calls but never reproduces their effect. Symptoms: white/black/wrong textures or material corruption.
2. **Wrong channel order**: diffuse/normal/material/light slots are swapped. Symptoms: bizarre shading, metallic skin, broken normals, blown-out lighting.
3. **State leak**: correct affected component, broken later components. Cause: no restore.
4. **Premature restore**: only the first draw gets the desired texture set. Cause: restore inserted before all originally covered draws finish.
5. **Global replacement**: unrelated resources are rebound because suffix naming was treated as a universal schema.
6. **Line-ending churn**: whole-file diff makes review difficult and can disturb fragile tooling.
7. **False validation**: zero `RabbitFX` strings is reported as success even though behavior was not preserved.
8. **All-or-nothing refusal**: an effect call that cannot be reproduced stops the whole conversion, leaving proven setter groups untouched and the user asked for files they do not have.
9. **Silent effect loss**: an effect call is retired without the report naming the affected draws and the lost visual.

## Known example pattern

One WWMI mod package contained both RabbitFX-dependent body rendering and a sibling face INI with direct bindings. The sibling INI established this local mapping:

```text
Normal   -> ps-t0
Lightmap -> ps-t1
Material -> ps-t2
Diffuse  -> ps-t3
```

The successful conversion saved `ps-t0..3`, replaced each RabbitFX texture group with direct bindings in the original location, and restored the four slots before exiting each affected override section.

This example is evidence for that package family only. Re-derive the mapping for each new mod.
