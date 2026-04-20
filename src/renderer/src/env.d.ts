/// <reference types="vite/client" />

import type React from "react";

declare module "react" {
    namespace JSX {
        interface IntrinsicElements {
            "model-viewer": React.DetailedHTMLProps<
                React.HTMLAttributes<HTMLElement>,
                HTMLElement
            > & {
                src?: string;
                alt?: string;
                "camera-controls"?: boolean | string;
                ar?: boolean | string;
                "ar-modes"?: string;
                "tone-mapping"?: string;
                poster?: string;
                "auto-rotate"?: boolean | string;
                "environment-image"?: string;
                exposure?: string;
                "shadow-intensity"?: string;
                "interaction-prompt"?: string;
            };
        }
    }
}
