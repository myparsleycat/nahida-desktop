import type { ViewerAnimationClip } from "@shared/mod-viewer/types";

import { stripComment, type IniSections } from "./ini";

const MAX_RANGE_LENGTH = 4096;

type PresentPattern = {
    variableId: string;
    speedToken: string;
    frameStartToken: string;
    frameEndToken: string;
};

export function extractPresentAnimations(
    sections: IniSections,
    defaults: Record<string, string>,
    manualVars: Iterable<string>,
    varPrefix?: string,
): ViewerAnimationClip[] {
    const present = Object.entries(sections).find(([name]) => name.toLowerCase() === "present");
    if (!present) {
        return [];
    }

    const manual = new Set([...manualVars].map((variable) => variable.toLowerCase()));
    const presentLines = present[1].map((line) => stripComment(line.text)).filter(Boolean);
    const discovered = new Map<string, ViewerAnimationClip>();

    for (const line of presentLines) {
        const assignment = /^(?:post\s+)?\$([\w.]+)\s*=\s*(.+)$/i.exec(line);
        if (!assignment) {
            continue;
        }
        const variableId = assignment[1];
        const expression = assignment[2].trim();
        if (!/\btime\b/i.test(expression) || manual.has(variableId.toLowerCase())) {
            continue;
        }
        const branchValues = collectDiscreteBranchValues(sections, variableId);
        if (branchValues.length < 2) {
            continue;
        }
        const fps = resolveAnimationFps(defaults, expression, varPrefix);
        if (!Number.isFinite(fps) || fps <= 0) {
            continue;
        }
        const clip = buildClip(
            variableId,
            fps,
            branchValues[0],
            branchValues[branchValues.length - 1],
            varPrefix,
        );
        if (clip) {
            discovered.set(clip.id, clip);
        }
    }

    for (const pattern of [
        ...collectPresentModuloPatterns(presentLines, defaults, varPrefix),
        ...collectPresentAccumulatorPatterns(presentLines, defaults, varPrefix),
    ]) {
        if (
            manual.has(pattern.variableId.toLowerCase()) ||
            discovered.has(prefixedId(pattern.variableId, varPrefix))
        ) {
            continue;
        }
        const branchValues = collectDiscreteBranchValues(sections, pattern.variableId);
        if (branchValues.length < 2) {
            continue;
        }
        const frameStart =
            resolveNumericToken(pattern.frameStartToken, defaults, varPrefix) ??
            branchValues[0] ??
            0;
        const frameEnd =
            resolveNumericToken(pattern.frameEndToken, defaults, varPrefix) ??
            branchValues[branchValues.length - 1] ??
            frameStart;
        if (
            !Number.isInteger(frameStart) ||
            !Number.isInteger(frameEnd) ||
            frameEnd <= frameStart
        ) {
            continue;
        }
        const speed = resolveNumericToken(pattern.speedToken, defaults, varPrefix);
        if (speed === null || !Number.isFinite(speed) || speed <= 0) {
            continue;
        }
        const fps = 60 / speed;
        if (!Number.isFinite(fps) || fps <= 0) {
            continue;
        }
        const clip = buildClip(pattern.variableId, fps, frameStart, frameEnd, varPrefix);
        if (clip) {
            discovered.set(clip.id, clip);
        }
    }

    return [...discovered.values()].sort((left, right) => left.id.localeCompare(right.id));
}

export function animationFrameValues(clip: ViewerAnimationClip): string[] {
    const values: string[] = [];
    for (let frame = clip.frameStart; frame <= clip.frameEnd; frame += 1) {
        values.push(String(frame));
    }
    return values;
}

function buildClip(
    variableId: string,
    fps: number,
    frameStart: number,
    frameEnd: number,
    varPrefix?: string,
): ViewerAnimationClip | null {
    if (!Number.isInteger(frameStart) || !Number.isInteger(frameEnd) || frameEnd <= frameStart) {
        return null;
    }
    const frameCount = frameEnd - frameStart + 1;
    if (!Number.isInteger(frameCount) || frameCount > MAX_RANGE_LENGTH) {
        return null;
    }
    const id = prefixedId(variableId, varPrefix);
    const frames = Array.from({ length: frameCount }, (_, offset) => {
        const frameIndex = frameStart + offset;
        return {
            index: frameIndex,
            time: (frameIndex - frameStart) / fps,
            values: { [id]: frameIndex },
        };
    });
    return {
        id,
        label: humanizeVariableLabel(variableId),
        variableIds: [id],
        fps,
        frameStart,
        frameEnd,
        loop: true,
        frames,
    };
}

function collectDiscreteBranchValues(sections: IniSections, variableId: string): number[] {
    const pattern = new RegExp(
        `^(?:if|elif|else if)\\s+\\$${escapeRegex(variableId)}\\s*==\\s*(-?\\d+(?:\\.\\d+)?)$`,
        "i",
    );
    const values = new Set<number>();
    for (const lines of Object.values(sections)) {
        for (const raw of lines) {
            const match = pattern.exec(stripComment(raw.text));
            if (!match) {
                continue;
            }
            const value = Number(match[1]);
            if (Number.isInteger(value)) {
                values.add(value);
            }
        }
    }
    return [...values].sort((left, right) => left - right);
}

function collectPresentModuloPatterns(
    lines: string[],
    defaults: Record<string, string>,
    varPrefix?: string,
): PresentPattern[] {
    const patterns = new Map<string, PresentPattern>();
    for (let index = 0; index < lines.length; index += 1) {
        const moduloMatch = /^if\s+\$([\w.]+)\s*%\s*(\$?[\w.-]+)\s*==\s*0$/i.exec(
            lines[index] ?? "",
        );
        if (!moduloMatch || !hasIncrementingAuxVariable(lines, moduloMatch[1])) {
            continue;
        }
        for (let probe = index + 1; probe < lines.length; probe += 1) {
            const compareMatch = /^(?:if|elif|else if)\s+\$([\w.]+)\s*<\s*(\$?[\w.-]+)\s*$/i.exec(
                lines[probe] ?? "",
            );
            if (!compareMatch) {
                continue;
            }
            const incrementPattern = new RegExp(
                `^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*\\$${escapeRegex(compareMatch[1])}\\s*\\+\\s*1$`,
                "i",
            );
            const rest = lines.slice(probe + 1);
            const resetMatch = rest
                .find((line) => {
                    return (
                        new RegExp(
                            `^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*(\\$?[\\w.-]+)$`,
                            "i",
                        ).test(line) && !incrementPattern.test(line)
                    );
                })
                ?.match(
                    new RegExp(`^\\$${escapeRegex(compareMatch[1])}\\s*=\\s*(\\$?[\\w.-]+)$`, "i"),
                );
            if (!rest.some((line) => incrementPattern.test(line)) || !resetMatch) {
                continue;
            }
            patterns.set(compareMatch[1], {
                variableId: compareMatch[1],
                speedToken: moduloMatch[2],
                frameStartToken: resetMatch[1],
                frameEndToken: compareMatch[2],
            });
            break;
        }
    }
    return [...patterns.values()].filter((pattern) => {
        const speed = resolveNumericToken(pattern.speedToken, defaults, varPrefix);
        return speed !== null && Number.isFinite(speed) && speed > 0;
    });
}

function collectPresentAccumulatorPatterns(
    lines: string[],
    defaults: Record<string, string>,
    varPrefix?: string,
): PresentPattern[] {
    const patterns = new Map<string, PresentPattern>();
    for (let index = 0; index < lines.length; index += 1) {
        const accumulatorMatch =
            /^if\s+\(\s*\$([\w.]+)\s*\+\s*\(?\s*1\s*\/\s*(\$?[\w.-]+)\s*\)?\s*\)\s*<\s*(\$?[\w.-]+)\s*$/i.exec(
                lines[index] ?? "",
            );
        if (!accumulatorMatch) {
            continue;
        }
        const auxVariableToken = accumulatorMatch[1];
        const speedToken = accumulatorMatch[2];
        const incrementPattern = new RegExp(
            `^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*\\+\\s*\\(?\\s*1\\s*\\/\\s*${escapeRegex(speedToken)}\\s*\\)?$`,
            "i",
        );
        const assignmentPattern = new RegExp(
            `^\\$([\\w.]+)\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*//\\s*1$`,
            "i",
        );
        const rest = lines.slice(index + 1);
        const resetMatch = rest
            .find((line) => {
                return (
                    new RegExp(
                        `^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*(\\$?[\\w.-]+)$`,
                        "i",
                    ).test(line) && !incrementPattern.test(line)
                );
            })
            ?.match(
                new RegExp(`^\\$${escapeRegex(auxVariableToken)}\\s*=\\s*(\\$?[\\w.-]+)$`, "i"),
            );
        const assignmentMatch = rest.find((line) => assignmentPattern.test(line));
        if (!rest.some((line) => incrementPattern.test(line)) || !resetMatch || !assignmentMatch) {
            continue;
        }
        const variableId = assignmentMatch.match(assignmentPattern)?.[1];
        if (!variableId) {
            continue;
        }
        patterns.set(variableId, {
            variableId,
            speedToken,
            frameStartToken: resetMatch[1],
            frameEndToken: accumulatorMatch[3],
        });
    }
    return [...patterns.values()].filter((pattern) => {
        const speed = resolveNumericToken(pattern.speedToken, defaults, varPrefix);
        return speed !== null && Number.isFinite(speed) && speed > 0;
    });
}

function hasIncrementingAuxVariable(lines: string[], auxVariableToken: string): boolean {
    return lines.some((line) =>
        new RegExp(
            `^(?:post\\s+)?\\$${escapeRegex(auxVariableToken)}\\s*=\\s*\\$${escapeRegex(auxVariableToken)}\\s*\\+\\s*1$`,
            "i",
        ).test(line),
    );
}

function resolveAnimationFps(
    defaults: Record<string, string>,
    expression: string,
    varPrefix?: string,
): number {
    const explicit = Number(lookupDefault(defaults, "fps", varPrefix));
    if (Number.isFinite(explicit) && explicit > 0) {
        return explicit;
    }
    const referenced = expression.match(/\$([\w.]*fps[\w.]*)/i)?.[1];
    if (!referenced) {
        return Number.NaN;
    }
    const fpsValue = Number(lookupDefault(defaults, referenced, varPrefix));
    return Number.isFinite(fpsValue) && fpsValue > 0 ? fpsValue : Number.NaN;
}

function resolveNumericToken(
    token: string | undefined,
    defaults: Record<string, string>,
    varPrefix?: string,
): number | null {
    if (!token) {
        return null;
    }
    const trimmed = token.trim();
    if (/^-?\d+(?:\.\d+)?$/.test(trimmed)) {
        const value = Number(trimmed);
        return Number.isFinite(value) ? value : null;
    }
    const name = trimmed.startsWith("$") ? trimmed.slice(1) : trimmed;
    const value = Number(lookupDefault(defaults, name, varPrefix));
    return Number.isFinite(value) ? value : null;
}

function lookupDefault(
    defaults: Record<string, string>,
    name: string,
    varPrefix?: string,
): string | undefined {
    const prefixed = varPrefix ? `${varPrefix}${name}` : name;
    return (
        defaults[prefixed] ??
        defaults[name] ??
        Object.entries(defaults).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1]
    );
}

function prefixedId(variableId: string, varPrefix?: string): string {
    return varPrefix ? `${varPrefix}${variableId}` : variableId;
}

function humanizeVariableLabel(id: string): string {
    return id
        .replace(/^\$+/, "")
        .split(/[._-]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ");
}

function escapeRegex(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
