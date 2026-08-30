import type { GLProps } from "@react-three/fiber";
import { WebGLRenderer, type WebGLRendererParameters } from "three";
import type { WebGPURendererParameters } from "three/webgpu";

export type ThreeRendererMode = "auto" | "webgl";

export type ThreeRendererOptions =
    | (Omit<WebGPURendererParameters, "canvas" | "forceWebGL" | "getFallback"> & {
          mode?: "auto";
      })
    | (Omit<WebGLRendererParameters, "canvas"> & {
          mode: "webgl";
      });

export function createThreeRenderer(options: ThreeRendererOptions = {}): GLProps {
    if (options.mode === "webgl") {
        const { mode: _mode, ...rendererOptions } = options;
        return (properties) =>
            new WebGLRenderer({
                ...properties,
                ...rendererOptions,
                // R3F declares a minimal OffscreenCanvas type, while three.js uses the DOM type.
                canvas: properties.canvas as HTMLCanvasElement | OffscreenCanvas,
            });
    }

    const { mode: _mode, ...rendererOptions } = options;
    return async (properties) => {
        const { WebGPURenderer } = await import("three/webgpu");
        const renderer = new WebGPURenderer({
            alpha: properties.alpha,
            antialias: properties.antialias,
            canvas: properties.canvas as HTMLCanvasElement | OffscreenCanvas,
            depth: properties.depth,
            logarithmicDepthBuffer: properties.logarithmicDepthBuffer,
            powerPreference:
                properties.powerPreference === "default" ? undefined : properties.powerPreference,
            stencil: properties.stencil,
            ...rendererOptions,
        });
        await renderer.init();
        return renderer;
    };
}
