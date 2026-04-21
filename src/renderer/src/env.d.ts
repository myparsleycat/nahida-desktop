/// <reference types="vite/client" />

import type { DetailedHTMLProps, HTMLAttributes } from "react";
import "react";
import type { ModelViewerElement } from "@google/model-viewer";

type ModelViewerProps = DetailedHTMLProps<
  HTMLAttributes<ModelViewerElement>,
  ModelViewerElement
> & {
  src?: string;
  ar?: boolean;
  exposure?: number | string;
  orientation?: string;
  "ar-modes"?: string;
  "camera-controls"?: boolean;
  "interaction-prompt"?: "auto" | "none";
  "tone-mapping"?: string;
  "shadow-intensity"?: number | string;
};

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "model-viewer": ModelViewerProps;
    }
  }
}
