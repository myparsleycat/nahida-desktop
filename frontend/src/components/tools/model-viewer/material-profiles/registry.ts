import type { ModViewerTransport, ViewerMaterialProfile } from "@shared/mod-viewer/types";
import { isPlainObject } from "es-toolkit";

import type {
    ModelViewerMaterialProfile,
    ModelViewerToonStyleBase,
    ModelViewerToonStyles,
} from "./profile";

import { efmiMaterialProfile } from "./efmi";
import { gimiMaterialProfile } from "./gimi";
import { rabbitFXMaterialProfile } from "./rabbitfx";
import { srmiMaterialProfile } from "./srmi";
import { zzmiMaterialProfile } from "./zzmi";

const genericMaterialProfile: ModelViewerMaterialProfile = {
    baseMaterial: { metalness: 0.05, roughness: 0.65 },
    derivativeTangentFrame: false,
    toonShadows: false,
};

// Keyed by every profile the backend can report, so a new Go profile fails to
// compile here until it has a renderer module.
const materialProfiles: Record<ViewerMaterialProfile, ModelViewerMaterialProfile> = {
    zzmi: zzmiMaterialProfile,
    "wuwa:rabbitfx": rabbitFXMaterialProfile,
    efmi: efmiMaterialProfile,
    gimi: gimiMaterialProfile,
    srmi: srmiMaterialProfile,
};

export function materialProfileOf(
    id: ModViewerTransport["materialProfile"],
): ModelViewerMaterialProfile {
    return (id && materialProfiles[id]) || genericMaterialProfile;
}

/** The style for a profile, or undefined when it has no toon shading. */
export function modelViewerToonStyleOf(
    styles: ModelViewerToonStyles | undefined,
    id: ModViewerTransport["materialProfile"],
): ModelViewerToonStyleBase | undefined {
    const toon = materialProfileOf(id).toon;
    if (!id || !toon) {
        return undefined;
    }
    return styles?.[id] ?? toon.defaultStyle;
}

export function parseModelViewerToonStyles(stored: string): ModelViewerToonStyles {
    const record = parseRecord(stored);
    return Object.fromEntries(
        toonProfiles().map(([id, toon]) => [id, toon.parse(asRecord(record[id]))]),
    );
}

export function serializeModelViewerToonStyles(styles: ModelViewerToonStyles): string {
    return JSON.stringify(
        Object.fromEntries(
            toonProfiles().flatMap(([id, toon]) => {
                const style = styles[id];
                return style ? [[id, toon.serialize(style)]] : [];
            }),
        ),
    );
}

function toonProfiles() {
    return (Object.keys(materialProfiles) as ViewerMaterialProfile[]).flatMap((id) => {
        const toon = materialProfiles[id].toon;
        return toon ? [[id, toon] as const] : [];
    });
}

function parseRecord(stored: string): Record<string, unknown> {
    try {
        return asRecord(JSON.parse(stored));
    } catch {
        return {};
    }
}

function asRecord(value: unknown): Record<string, unknown> {
    return isPlainObject(value) ? value : {};
}
