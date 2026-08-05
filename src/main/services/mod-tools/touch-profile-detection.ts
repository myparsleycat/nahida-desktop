import path from "node:path";

import { loadIniBundle } from "@main/lib/mod-static-glb/ini-loader";
import fse from "fs-extra";

import {
    TOUCH_PROFILE_MANIFEST_FILE,
    TOUCH_PROFILE_MANIFEST_KIND,
    TOUCH_SHADER_FILES,
} from "./touch-profile-types";

export type TouchProfileInputStatus = "none" | "generated" | "incomplete" | "suspected";

export type TouchProfileInputDetection = {
    status: TouchProfileInputStatus;
    reasons: string[];
    namespace?: string;
};

export async function inspectTouchProfileInput(modPath: string) {
    const bundle = await loadIniBundle(modPath);
    return inspectTouchProfileBundle(path.dirname(bundle.iniPath), bundle.sourcePaths);
}

export async function inspectTouchProfileBundle(
    modRoot: string,
    sourcePaths: string[],
): Promise<TouchProfileInputDetection> {
    const root = path.resolve(modRoot);
    const manifest = await readManifest(path.join(root, TOUCH_PROFILE_MANIFEST_FILE));
    if (manifest) {
        return withRuntimeFileStatus(root, {
            status: "generated",
            reasons: ["Nahida Touch Profile manifest found"],
        });
    }

    const iniText = (
        await Promise.all(
            [...new Set(sourcePaths)].map((sourcePath) => fse.readFile(sourcePath, "utf8")),
        )
    ).join("\n");
    const stateNamespaces = extractNamespaces(
        iniText,
        /Nahida Touch Profile state\s*\(\s*([^\s)]+)\s*\)/gi,
    );
    const runtimeNamespaces = extractNamespaces(
        iniText,
        /Nahida Touch Profile runtime\s*\(\s*([^\s)]+)\s*\)/gi,
    );
    const matchingNamespace = stateNamespaces.find((namespace) =>
        runtimeNamespaces.includes(namespace),
    );

    if (matchingNamespace) {
        return withRuntimeFileStatus(root, {
            status: "generated",
            namespace: matchingNamespace,
            reasons: ["Nahida Touch Profile INI markers found"],
        });
    }

    if (stateNamespaces.length > 0 || runtimeNamespaces.length > 0) {
        return withRuntimeFileStatus(root, {
            status: "incomplete",
            reasons: ["Incomplete Nahida Touch Profile INI markers found"],
        });
    }

    const missingShaders = await missingRuntimeShaders(root);
    const hasRuntimeReferences = [
        "rzm_gs_probe.hlsl",
        "rzm_object_detect.hlsl",
        "rzm_jiggle_interaction.hlsl",
    ].every((fileName) => iniText.toLowerCase().includes(fileName.toLowerCase()));
    if (missingShaders.length === 0 && hasRuntimeReferences) {
        return {
            status: "suspected",
            reasons: ["Touch runtime shaders and INI references found"],
        };
    }

    return { status: "none", reasons: [] };
}

export async function assertTouchProfileInputAllowed(modPath: string) {
    const detection = await inspectTouchProfileInput(modPath);
    assertTouchProfileDetectionAllowed(modPath, detection);
}

export function assertTouchProfileDetectionAllowed(
    modPath: string,
    detection: TouchProfileInputDetection,
) {
    if (detection.status === "none") return;

    const code =
        detection.status === "suspected"
            ? "TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH"
            : "TOUCH_PROFILE_INPUT_ALREADY_TOUCH";
    const message =
        detection.status === "suspected"
            ? "Input appears to be an existing Touch mod and cannot be safely converted."
            : "Input is already a Nahida Touch Profile mod and cannot be converted again.";
    const details = detection.reasons.length > 0 ? ` ${detection.reasons.join("; ")}.` : "";
    throw new Error(`${code}: ${message}${details} Path: ${path.resolve(modPath)}`);
}

async function readManifest(manifestPath: string) {
    if (!(await fse.pathExists(manifestPath))) return false;

    try {
        const manifest = await fse.readJson(manifestPath);
        return manifest?.kind === TOUCH_PROFILE_MANIFEST_KIND;
    } catch {
        return false;
    }
}

async function withRuntimeFileStatus(
    root: string,
    detection: TouchProfileInputDetection,
): Promise<TouchProfileInputDetection> {
    const missingShaders = await missingRuntimeShaders(root);
    if (missingShaders.length === 0) return detection;

    return {
        ...detection,
        status: "incomplete",
        reasons: [...detection.reasons, `Missing runtime shaders: ${missingShaders.join(", ")}`],
    };
}

async function missingRuntimeShaders(root: string) {
    const runtimeRoot = path.join(root, "Resources", "IM");
    const existing = await Promise.all(
        TOUCH_SHADER_FILES.map(async (fileName) =>
            (await fse.pathExists(path.join(runtimeRoot, fileName))) ? null : fileName,
        ),
    );
    return existing.filter((fileName) => fileName !== null);
}

function extractNamespaces(text: string, pattern: RegExp) {
    return [...text.matchAll(pattern)].map((match) => match[1].toLowerCase());
}
