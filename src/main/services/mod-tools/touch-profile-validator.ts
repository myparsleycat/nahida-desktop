import path from "node:path";

import fse from "fs-extra";

import type { TouchGeneratedAssets } from "./touch-profile-assets";

import { normalizeTouchZoneSettings, resolveTouchJiggleParams } from "./touch-profile-settings";
import {
    DEFAULT_TOUCH_JIGGLE_PARAMS,
    TOUCH_SHADER_FILES,
    type TouchComponentAnalysis,
    type TouchComponentDraft,
    type TouchValidationIssue,
    type TouchValidationResult,
} from "./touch-profile-types";

export async function validateTouchOutput(input: {
    outputRoot: string;
    iniPath: string;
    components: TouchComponentAnalysis[];
    drafts: TouchComponentDraft[];
    assets: TouchGeneratedAssets[];
}): Promise<TouchValidationResult> {
    const issues: TouchValidationIssue[] = [];
    const iniText = await fse.readFile(input.iniPath, "utf8");

    for (const shader of TOUCH_SHADER_FILES) {
        const shaderPath = path.join(input.outputRoot, "Resources", "IM", shader);
        if (!(await fse.pathExists(shaderPath))) {
            issues.push({
                level: "error",
                code: "missing_shader",
                message: `Missing runtime shader: ${shader}`,
            });
        }
    }

    const sectionHeaders = [...iniText.matchAll(/^\s*\[([^\]]+)\]\s*$/gm)].map((match) =>
        match[1].trim().toLowerCase(),
    );
    const duplicates = sectionHeaders.filter(
        (header, index) => sectionHeaders.indexOf(header) !== index,
    );
    if (duplicates.length > 0) {
        issues.push({
            level: "error",
            code: "duplicate_ini_section",
            message: `Duplicate INI sections: ${[...new Set(duplicates)].join(", ")}`,
        });
    }

    const channelSettings = new Map<number, string>();
    for (const draft of input.drafts) {
        if (!draft.interactive) continue;
        for (const zone of draft.zones) {
            let normalizedSettings;
            try {
                normalizedSettings = normalizeTouchZoneSettings(zone.settings);
            } catch (error) {
                issues.push({
                    level: "error",
                    code: "invalid_zone_settings",
                    componentId: draft.componentId,
                    message: `Invalid settings for ${zone.id}: ${
                        error instanceof Error ? error.message : String(error)
                    }`,
                });
                continue;
            }

            const params = resolveTouchJiggleParams(normalizedSettings, draft.objectId);
            const fingerprint = JSON.stringify({
                radius: params.radius,
                strength: params.strength,
                falloff: params.falloff,
                maxOffset: params.maxOffset,
                damping: params.grabDamping / DEFAULT_TOUCH_JIGGLE_PARAMS.grabDamping,
                spring: params.grabSpring / DEFAULT_TOUCH_JIGGLE_PARAMS.grabSpring,
            });
            const previous = channelSettings.get(zone.channel);
            if (previous && previous !== fingerprint) {
                issues.push({
                    level: "error",
                    code: "conflicting_zone_channel",
                    componentId: draft.componentId,
                    message: `Conflicting settings for runtime zone channel ${zone.channel}`,
                });
            } else {
                channelSettings.set(zone.channel, fingerprint);
            }
        }
    }

    for (const asset of input.assets) {
        const component = input.components.find((entry) => entry.id === asset.componentId);
        const draft = input.drafts.find((entry) => entry.componentId === asset.componentId);
        if (!component || !draft?.interactive) continue;

        // Zones 0-3 live in Masks0; Masks1/2 are often all-zero and still valid.
        let totalNonzero = 0;
        for (const maskPath of asset.maskPaths) {
            const absolute = path.join(input.outputRoot, maskPath);
            if (!(await fse.pathExists(absolute))) {
                issues.push({
                    level: "error",
                    code: "missing_mask",
                    componentId: component.id,
                    message: `Missing mask file: ${maskPath}`,
                });
                continue;
            }

            const bytes = await fse.readFile(absolute);
            const expected = component.vertexCount * 16;
            if (bytes.byteLength !== expected) {
                issues.push({
                    level: "error",
                    code: "mask_size",
                    componentId: component.id,
                    message: `Mask size mismatch for ${maskPath}: ${bytes.byteLength} != ${expected}`,
                });
            }

            const values = new Float32Array(
                bytes.buffer,
                bytes.byteOffset,
                Math.floor(bytes.byteLength / 4),
            );
            for (const value of values) {
                if (!Number.isFinite(value) || value < 0 || value > 1) {
                    issues.push({
                        level: "error",
                        code: "mask_value",
                        componentId: component.id,
                        message: `Mask contains invalid value in ${maskPath}`,
                    });
                    break;
                }
                if (value > 0) totalNonzero += 1;
            }
        }
        if (totalNonzero === 0) {
            issues.push({
                level: "error",
                code: "empty_mask",
                componentId: component.id,
                message: `No active mask weights across Masks0/1/2 for ${component.name}`,
            });
        }

        for (const objectMap of asset.objectMapPaths) {
            if (!(await fse.pathExists(objectMap.absolutePath))) {
                issues.push({
                    level: "error",
                    code: "missing_object_map",
                    componentId: component.id,
                    message: `Missing ObjectMap: ${objectMap.relativePath}`,
                });
                continue;
            }
            const bytes = await fse.readFile(objectMap.absolutePath);
            if (bytes.byteLength < 32 || bytes.byteLength % 16 !== 0) {
                issues.push({
                    level: "error",
                    code: "object_map_size",
                    componentId: component.id,
                    message: `Invalid ObjectMap size: ${objectMap.relativePath}`,
                });
            }
        }

        if (!(await fse.pathExists(asset.paramsAbsolutePath))) {
            issues.push({
                level: "error",
                code: "missing_params",
                componentId: component.id,
                message: `Missing params: ${asset.paramsRelativePath}`,
            });
        } else {
            const paramsStat = await fse.stat(asset.paramsAbsolutePath);
            if (paramsStat.size !== 64) {
                issues.push({
                    level: "error",
                    code: "params_size",
                    componentId: component.id,
                    message: `Params size must be 64 bytes: ${asset.paramsRelativePath}`,
                });
            }
        }

        const requiredSnippets = [
            `Resource${path.basename(asset.maskPaths[0] || "").replace(/\W/g, "")}`,
            "rzm_jiggle_interaction.hlsl",
            "rzm_object_detect.hlsl",
            `dispatch = (${component.vertexCount} + 255) // 256, 1, 1`,
        ];
        for (const snippet of requiredSnippets.slice(1)) {
            if (!iniText.includes(snippet)) {
                issues.push({
                    level: "error",
                    code: "ini_missing_snippet",
                    componentId: component.id,
                    message: `INI missing required snippet: ${snippet}`,
                });
            }
        }
    }

    return {
        ok: !issues.some((issue) => issue.level === "error"),
        issues,
    };
}
