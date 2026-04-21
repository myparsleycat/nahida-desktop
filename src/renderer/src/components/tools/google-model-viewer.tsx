import "@google/model-viewer";
import { cn } from "@renderer/lib/utils";
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import type { ModelViewerHandle, ModelViewerSurfaceProps } from "./model-viewer-contract";
import {
  applyAngledViewerFraming,
  captureViewerCameraState,
  restoreViewerCameraState,
  suppressModelViewerFocusOutline,
  type ModelViewerElement,
} from "./model-viewer-session";

export const GoogleModelViewer = forwardRef<ModelViewerHandle, ModelViewerSurfaceProps>(
  function GoogleModelViewer({ className, onError, onLoad, orientation, src }, ref) {
    const elementRef = useRef<ModelViewerElement | null>(null);

    useImperativeHandle(
      ref,
      () => ({
        captureCameraState: () => captureViewerCameraState(elementRef.current),
        restoreCameraState: (state, options) => {
          restoreViewerCameraState(elementRef.current, state, options);
        },
        setDoubleSided: async (doubleSided) => {
          const materials = elementRef.current?.model?.materials;
          if (!materials?.length) {
            return;
          }

          await Promise.allSettled(
            materials.map(async (material) => {
              await material.ensureLoaded?.();
              material.setDoubleSided?.(doubleSided);
            }),
          );
        },
        updateFraming: async () => {
          await elementRef.current?.updateFraming?.();
          applyAngledViewerFraming(elementRef.current);
        },
      }),
      [],
    );

    useEffect(() => {
      const element = elementRef.current;
      if (!element) {
        return;
      }

      suppressModelViewerFocusOutline(element);

      const handleLoad = () => onLoad?.();
      const handleError = (event: Event) => onError?.(event);
      element.addEventListener("load", handleLoad);
      element.addEventListener("error", handleError);

      return () => {
        element.removeEventListener("load", handleLoad);
        element.removeEventListener("error", handleError);
      };
    }, [onError, onLoad]);

    return (
      <model-viewer
        ref={(element) => {
          elementRef.current = element as ModelViewerElement | null;
          if (element) {
            suppressModelViewerFocusOutline(element);
          }
        }}
        className={cn("h-full w-full", className)}
        tabIndex={-1}
        src={src}
        camera-controls
        interaction-prompt="none"
        tone-mapping="neutral"
        shadow-intensity="1"
        exposure="1"
        orientation={orientation}
      >
        <div className="progress-bar hide" slot="progress-bar">
          <div className="update-bar"></div>
        </div>
      </model-viewer>
    );
  },
);
