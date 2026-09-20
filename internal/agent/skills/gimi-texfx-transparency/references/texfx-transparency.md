# TexFx: distinguish three different defects

Check the installed implementation first. The following mechanism was observed in TexFx 1.0661; it is a diagnostic lead, not an unconditional rule for every version or transparency implementation.

## Missing band near the start of a gradient

The affected INI bound a DDS mask to `ps-t69` and called `CommandList\TexFx\T` only when its hair-transparency toggle was enabled. Geometry and animation resources were otherwise unchanged. The mask's DX10 format was BC7_UNORM_SRGB (99), rather than BC7_UNORM (98).

In that implementation:

- TexFx used the red channel for transparency; alpha carried a different effect.
- The normal pass discarded nonzero red to let the transparent pass draw it.
- The transparent pass wrote the sampled red into a temporary render target's alpha.
- The final overlay discarded alpha equal to zero (and one).

An sRGB red byte of 1..6 decodes to approximately 0.000304..0.001821. If the temporary alpha is 8-bit UNORM, these nonzero values round to zero. Both passes can then omit the same region. Red 7 rounds to 1/255. This threshold assumes point sampling and 8-bit alpha; filtering, compression, another buffer format, or different shader logic changes it. Prove the format with a capture when available, or label it as a conditional explanation supported by controlled trials.

The example's UV analysis mapped these values to the missing band, and a lossless UNORM retag removed the band in a user reload. Do not generalize its paths, index counts, component IDs, height thresholds, or DDS channel selection.

## Stronger transparency after a retag

Changing a format tag does not change the stored byte values. Without the sRGB decode, red 128 is about 0.502 instead of 0.216. In a red-as-transparency mask, this makes the region much more transparent. Retagging alone is useful as a small reversible test but does not preserve the original curve.

To preserve sampled values, bake the sRGB transfer into a linear RGBA8 mask. Quantize before the shader's branch so values that become zero take the opaque path. Preserve alpha. Avoid lossy recompression when reasoning about near-zero thresholds. Do not apply this conversion twice or to a color diffuse map just because it uses sRGB.

## A remaining opaque/transparent color seam

If geometry continuity is restored and the strength of transparency is acceptable, a sharp color seam exactly where red changes from zero to nonzero can indicate mismatched shading/composition. Even a near-zero transparency may select a different color source from the normal pass. Inspect the installed overlay's inputs and outputs; do not assert a missing lighting stage from a screenshot alone.

A selected region's minimum red of 1/255 can keep both upper and lower portions on the same transparent path. The expected result is a more continuous color, with possibly flatter or otherwise different lighting over the whole selected part. Exclude unrelated atlas regions and verify shared UVs before this workaround. It cannot reproduce the opaque pipeline's original lighting, and it is not a universal fix for sorting, depth, backface, or draw-range defects.

## Sources and evidentiary limits

- TexFx source: https://github.com/SinsOfSeven/TexFx — compare with the user's installed files; the installed implementation is the execution evidence.
- DXGI format definitions: https://learn.microsoft.com/en-us/windows/win32/api/dxgiformat/ne-dxgiformat-dxgi_format
- sRGB resource-view conversion: https://learn.microsoft.com/en-us/windows/win32/direct3ddxgi/converting-data-color-space
- Related color report: https://github.com/SinsOfSeven/TexFx/issues/43 — a reported symptom, not proof of this cause or an established fix.
