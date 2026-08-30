import type { GLProps } from "@react-three/fiber";
import { WebGLRenderer, type WebGLRendererParameters } from "three";
import type { WebGPURendererParameters } from "three/webgpu";

type RendererFactory = Extract<GLProps, (...args: never[]) => unknown>;
type RendererProperties = Parameters<RendererFactory>[0];

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
    const initializations = new WeakMap<object, ReturnType<typeof initialize>>();
    return (properties) => {
        const canvas = properties.canvas as HTMLCanvasElement | OffscreenCanvas;
        const pending = initializations.get(canvas);
        if (pending) {
            return pending;
        }

        const initialization = initialize(canvas, properties);
        initializations.set(canvas, initialization);
        const clearInitialization = () => {
            // Retain the settled promise until R3F's await continuation assigns state.gl.
            queueMicrotask(() => {
                if (initializations.get(canvas) === initialization) {
                    initializations.delete(canvas);
                }
            });
        };
        void initialization.then(clearInitialization, clearInitialization);
        return initialization;
    };

    async function initialize(
        canvas: HTMLCanvasElement | OffscreenCanvas,
        properties: RendererProperties,
    ) {
        const { WebGPURenderer } = await import("three/webgpu");
        const renderer = new WebGPURenderer({
            alpha: properties.alpha,
            antialias: properties.antialias,
            canvas,
            depth: properties.depth,
            logarithmicDepthBuffer: properties.logarithmicDepthBuffer,
            powerPreference:
                properties.powerPreference === "default" ? undefined : properties.powerPreference,
            stencil: properties.stencil,
            ...rendererOptions,
        });
        await renderer.init();
        return renderer;
    }
}
