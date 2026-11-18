# SPDX-License-Identifier: GPL-3.0-or-later
"""Converts Image.pixels values to the RGBA8 rows that TextureBegin describes.

This module imports nothing from Blender so the transport's worker thread can run it while Blender keeps
working.
"""

import numpy


def to_rgba8(values, width, height, channels, encode_srgb):
    """Returns the RGBA8 bytes, rows from the top, of values: width * height * channels floats from
    Image.pixels, rows from the bottom. A byte image's values are its stored bytes over 255, which this returns
    unchanged. encode_srgb applies the sRGB transfer to the color channels of a float image that holds scene
    linear color, so the result reads like an 8-bit sRGB file. values is overwritten."""
    pixels = values.reshape(height, width, channels)
    numpy.clip(pixels, 0.0, 1.0, out=pixels)
    if encode_srgb:
        color = pixels[..., : min(channels, 3)]
        color[...] = numpy.where(
            color <= 0.0031308, color * 12.92, 1.055 * numpy.power(color, 1 / 2.4) - 0.055
        )
    numpy.multiply(pixels, 255.0, out=pixels)
    numpy.add(pixels, 0.5, out=pixels)
    quantized = pixels.astype(numpy.uint8)

    if channels == 4:
        rgba = quantized
    else:
        rgba = numpy.empty((height, width, 4), dtype=numpy.uint8)
        if channels >= 3:
            rgba[..., :3] = quantized[..., :3]
            rgba[..., 3] = 255
        else:
            rgba[..., :3] = quantized[..., :1]
            rgba[..., 3] = quantized[..., 1] if channels == 2 else 255
    return numpy.ascontiguousarray(rgba[::-1]).tobytes()
