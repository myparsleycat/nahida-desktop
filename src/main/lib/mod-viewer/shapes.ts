import { canonicalVarNames, resourceLookup, type IniSections, type ResourceInfo } from "./ini";

const VALUE_RE = /^x\d+\s*=\s*\$(\w+)\s*$/i;
const BUFFER_RE = /^cs-t\d+\s*=\s*copy\s+(\S+)\s*$/i;
const SHAPE_BUFFER_RE = /^cs-t(50|51)\s*=\s*copy\s+(\S+)\s*$/i;
const SHAPE_X_RE = /^x88\s*=\s*(.+?)\s*$/i;
const NEGATED_VALUE_RE = /^\$(\w+)\s*\*\s*-1$/i;
const REMAP_RE = /^\$(\w+)\s*=\s*\(?\s*\$(\w+)\s*\*\s*2\s*-\s*1\s*\)?$/i;
const SLIDER_RE = /^x87\s*=\s*\$(\w+)\s*\*\s*x87\s*$/i;
const SLIDER_ANY_RE = /^x87\s*=.*\$(\w+)\s*$/i;
const SHAPE_ID_RE = /^\$\\WWMIv1\\shapekey_id\s*=\s*(\d+)\s*$/i;
const SHAPE_VALUE_RE = /^\$\\WWMIv1\\shapekey_value\s*=\s*\$(\w+)\s*$/i;
const BIND_RE = /^(cs-t(?:0|1|6|33))\s*=\s*(?:copy\s+|ref\s+)?(\S+)\s*$/i;
const BATCH_OFFSET_RE = /^global\s+\$shapekey_vertex_offset_batch(\d+)\s*=\s*(\d+)\s*$/i;

export type ShapeSlider = {
    kind: "shape_slider";
    name: string;
    var: string;
    min: number;
    max: number;
    step: number;
    baseFile?: string;
    targetFile?: string;
    lowFile?: string;
    stride?: number;
    mode?: "midpoint_pair";
    source?: string;
    iniPath?: string;
    section: string;
    shapeId?: number;
    bufferShapeId?: number;
    sparseEntryOffset?: number;
    offsetFile?: string;
    vertexIdFile?: string;
    vertexOffsetFile?: string;
};

export function extractShapeSliders(
    sections: IniSections,
    resources: Record<string, ResourceInfo>,
    varPrefix?: string,
    source?: string,
    canonicalVars?: Record<string, string>,
): ShapeSlider[] {
    const canon = canonicalVars ?? canonicalVarNames(sections);
    const resource = (name?: string) => resourceLookup(resources, name);
    const found: ShapeSlider[] = [];
    const prefix = varPrefix ?? "";

    const authoredSliderVars = new Set<string>();
    const remappedVars: Record<string, string> = {};
    for (const [section, lines] of Object.entries(sections)) {
        for (const raw of lines) {
            const line = raw.text.split(";")[0].trim();
            if (section.toLowerCase().startsWith("commandlistdrawslider")) {
                const slider = SLIDER_RE.exec(line);
                if (slider) {
                    authoredSliderVars.add(
                        (canon[slider[1].toLowerCase()] ?? slider[1]).toLowerCase(),
                    );
                }
            }
            const remap = REMAP_RE.exec(line);
            if (remap) {
                remappedVars[(canon[remap[1].toLowerCase()] ?? remap[1]).toLowerCase()] =
                    canon[remap[2].toLowerCase()] ?? remap[2];
            }
        }
    }

    for (const [section, lines] of Object.entries(sections)) {
        if (!section.toLowerCase().startsWith("customshader")) {
            continue;
        }
        let variable: string | undefined;
        const bufferNames: string[] = [];
        for (const raw of lines) {
            const line = raw.text.split(";")[0].trim();
            const value = VALUE_RE.exec(line);
            if (value && variable === undefined) {
                variable = canon[value[1].toLowerCase()] ?? value[1];
                continue;
            }
            const buffer = BUFFER_RE.exec(line);
            if (buffer) {
                bufferNames.push(buffer[1]);
            }
        }
        if (
            !variable ||
            bufferNames.length < 2 ||
            !bufferNames[0].toLowerCase().endsWith(".base")
        ) {
            continue;
        }
        const base = resource(bufferNames[0]);
        const target = resource(bufferNames[1]);
        if (!base.filename || !target.filename) {
            continue;
        }
        const baseStride = base.stride ?? 40;
        const targetStride = target.stride ?? baseStride;
        if (baseStride !== targetStride || baseStride < 12) {
            continue;
        }
        found.push({
            kind: "shape_slider",
            name: variable,
            var: `${prefix}${variable}`,
            min: 0,
            max: 1,
            step: 0.01,
            baseFile: base.filename,
            targetFile: target.filename,
            stride: baseStride,
            source,
            iniPath: lines[0]?.iniPath,
            section,
        });
    }

    for (const [section, lines] of Object.entries(sections)) {
        if (!section.toLowerCase().startsWith("customshader")) {
            continue;
        }
        const candidates: Array<{ variable: string; baseName: string; targetName: string }> = [];
        const remappedTargets: Record<
            string,
            { var: string; base: string; low?: string; high?: string }
        > = {};
        let variable: string | undefined;
        let baseName: string | undefined;
        let remapSide: "low" | "high" | undefined;
        for (const raw of lines) {
            const line = raw.text.split(";")[0].trim();
            const xValue = SHAPE_X_RE.exec(line);
            if (xValue) {
                const value = xValue[1].trim();
                const match = /^\$(\w+)$/.exec(value);
                const negative = NEGATED_VALUE_RE.exec(value);
                if (match || negative) {
                    const token = (match ?? negative)![1];
                    variable = canon[token.toLowerCase()] ?? token;
                    remapSide = negative ? "low" : "high";
                } else {
                    variable = undefined;
                    remapSide = undefined;
                }
                continue;
            }
            const buffer = SHAPE_BUFFER_RE.exec(line);
            if (!buffer) {
                continue;
            }
            if (buffer[1] === "50") {
                baseName = buffer[2];
                continue;
            }
            if (!baseName || !variable) {
                continue;
            }
            const original = remappedVars[variable.toLowerCase()];
            if (original) {
                const pair = remappedTargets[variable.toLowerCase()] ?? {
                    var: original,
                    base: baseName,
                };
                if (pair.base.toLowerCase() === baseName.toLowerCase() && remapSide) {
                    pair[remapSide] = buffer[2];
                }
                remappedTargets[variable.toLowerCase()] = pair;
            } else {
                candidates.push({ variable, baseName, targetName: buffer[2] });
            }
        }

        const completeRemaps = Object.values(remappedTargets).filter(
            (pair) => pair.low && pair.high,
        );
        const baseKeys = new Set([
            ...candidates.map((item) => item.baseName.toLowerCase()),
            ...completeRemaps.map((pair) => pair.base.toLowerCase()),
        ]);
        if (candidates.length + completeRemaps.length < 2 || baseKeys.size !== 1) {
            continue;
        }
        const existingPairs = new Set(
            found.map((item) => `${item.var.toLowerCase()}|${item.baseFile}|${item.targetFile}`),
        );
        for (const candidate of candidates) {
            if (
                authoredSliderVars.size > 0 &&
                !authoredSliderVars.has(candidate.variable.toLowerCase())
            ) {
                continue;
            }
            let base = resource(candidate.baseName);
            const target = resource(candidate.targetName);
            if (candidate.baseName.toLowerCase().endsWith(".b")) {
                const runtimeBase = resource(candidate.baseName.slice(0, -2));
                if (runtimeBase.filename) {
                    base = runtimeBase;
                }
            }
            const pair = `${prefix}${candidate.variable}`.toLowerCase();
            const identity = `${pair}|${base.filename}|${target.filename}`;
            const baseStride = base.stride ?? 40;
            const targetStride = target.stride ?? baseStride;
            if (
                !base.filename ||
                !target.filename ||
                existingPairs.has(identity) ||
                base.filename === target.filename ||
                baseStride !== targetStride ||
                baseStride < 12
            ) {
                continue;
            }
            found.push({
                kind: "shape_slider",
                name: candidate.variable,
                var: `${prefix}${candidate.variable}`,
                min: 0,
                max: 1,
                step: 0.01,
                baseFile: base.filename,
                targetFile: target.filename,
                stride: baseStride,
                source,
                iniPath: lines[0]?.iniPath,
                section,
            });
            existingPairs.add(identity);
        }
        for (const item of completeRemaps) {
            if (authoredSliderVars.size > 0 && !authoredSliderVars.has(item.var.toLowerCase())) {
                continue;
            }
            let base = resource(item.base);
            if (item.base.toLowerCase().endsWith(".b")) {
                const runtimeBase = resource(item.base.slice(0, -2));
                if (runtimeBase.filename) {
                    base = runtimeBase;
                }
            }
            const low = resource(item.low);
            const high = resource(item.high);
            const strides = new Set([base.stride ?? 40, low.stride ?? 40, high.stride ?? 40]);
            if (!base.filename || !low.filename || !high.filename || strides.size !== 1) {
                continue;
            }
            const stride = [...strides][0];
            if (stride < 12) {
                continue;
            }
            found.push({
                kind: "shape_slider",
                mode: "midpoint_pair",
                name: item.var,
                var: `${prefix}${item.var}`,
                min: 0,
                max: 1,
                step: 0.01,
                baseFile: base.filename,
                lowFile: low.filename,
                targetFile: high.filename,
                stride,
                source,
                iniPath: lines[0]?.iniPath,
                section,
            });
        }
    }

    const sliders: Array<{ variable: string; section: string; iniPath?: string }> = [];
    const shapeIds: Record<string, number> = {};
    const bindings: Record<string, string> = {};
    const batchOffsets: Record<number, number> = {};
    for (const lines of Object.values(sections)) {
        for (const raw of lines) {
            const match = BATCH_OFFSET_RE.exec(raw.text.split(";")[0].trim());
            if (match) {
                batchOffsets[Number(match[1])] = Number(match[2]);
            }
        }
    }
    for (const [section, lines] of Object.entries(sections)) {
        const cleaned = lines.map((line) => line.text.split(";")[0].trim());
        if (section.toLowerCase().startsWith("commandlistdrawslider")) {
            for (const line of cleaned) {
                const match = SLIDER_RE.exec(line);
                if (match) {
                    sliders.push({
                        variable: canon[match[1].toLowerCase()] ?? match[1],
                        section,
                        iniPath: lines[0]?.iniPath,
                    });
                    break;
                }
            }
        }
        let pendingId: number | undefined;
        for (const line of cleaned) {
            const idMatch = SHAPE_ID_RE.exec(line);
            if (idMatch) {
                pendingId = Number(idMatch[1]);
                continue;
            }
            const valueMatch = SHAPE_VALUE_RE.exec(line);
            if (valueMatch && pendingId !== undefined) {
                shapeIds[(canon[valueMatch[1].toLowerCase()] ?? valueMatch[1]).toLowerCase()] =
                    pendingId;
                pendingId = undefined;
            }
            const bind = BIND_RE.exec(line);
            if (bind) {
                bindings[bind[1].toLowerCase()] ??= bind[2];
            }
        }
    }

    const sparseResources = {
        baseFile: resource(bindings["cs-t6"]).filename,
        offsetFile: resource(bindings["cs-t33"]).filename,
        vertexIdFile: resource(bindings["cs-t0"]).filename,
        vertexOffsetFile: resource(bindings["cs-t1"]).filename,
    };
    const sparseReady = Object.values(sparseResources).every(Boolean);
    const existing = new Set(found.map((item) => item.var.toLowerCase()));
    for (const slider of sliders) {
        const fullVar = `${prefix}${slider.variable}`;
        if (existing.has(fullVar.toLowerCase())) {
            continue;
        }
        const item: ShapeSlider = {
            kind: "shape_slider",
            name: slider.variable,
            var: fullVar,
            min: 0,
            max: 1,
            step: 0.01,
            source,
            iniPath: slider.iniPath,
            section: slider.section,
        };
        const shapeId = shapeIds[slider.variable.toLowerCase()];
        if (shapeId !== undefined && sparseReady) {
            const batch = Math.floor(shapeId / 127);
            Object.assign(item, sparseResources, {
                shapeId,
                bufferShapeId: shapeId + batch,
                sparseEntryOffset: batchOffsets[batch] ?? 0,
                stride: resource(bindings["cs-t6"]).stride ?? 12,
            });
        }
        found.push(item);
        existing.add(fullVar.toLowerCase());
    }

    const menuVars = new Set<string>();
    for (const [section, lines] of Object.entries(sections)) {
        if (!section.toLowerCase().startsWith("commandlist")) {
            continue;
        }
        for (const raw of lines) {
            const match = SLIDER_ANY_RE.exec(raw.text.split(";")[0].trim());
            if (match) {
                menuVars.add(canon[match[1].toLowerCase()] ?? match[1]);
            }
        }
    }

    const multiSets: Array<{
        buffers: Record<number, string>;
        scalarVars: Record<number, string>;
        lines: typeof found extends never ? never : (typeof sections)[string];
    }> = [];
    for (const lines of Object.values(sections)) {
        let current: Record<number, string> = {};
        const sectionSets: Array<Record<number, string>> = [];
        const scalarVars: Record<number, string> = {};
        for (const raw of lines) {
            const line = raw.text.split(";")[0].trim();
            const buffer = /^cs-t(5[0-4])\s*=\s*copy\s+(\S+)$/i.exec(line);
            if (buffer) {
                const slot = Number(buffer[1]) - 50;
                if (slot === 0) {
                    if (Object.keys(current).length === 5) {
                        sectionSets.push(current);
                    }
                    current = {};
                }
                current[slot] = buffer[2];
                continue;
            }
            const scalar = /^x(88|89)\s*=\s*\$(\w+)$/i.exec(line);
            if (scalar) {
                scalarVars[Number(scalar[1])] = canon[scalar[2].toLowerCase()] ?? scalar[2];
            }
        }
        if (Object.keys(current).length === 5) {
            sectionSets.push(current);
        }
        for (const buffers of sectionSets) {
            multiSets.push({ buffers, scalarVars: { ...scalarVars }, lines });
        }
    }

    const roles = { 88: [1, 2], 89: [3, 4] } as const;
    for (const set of multiSets) {
        const base = resource(set.buffers[0]);
        if (!base.filename) {
            continue;
        }
        for (const [register, [highSlot, lowSlot]] of Object.entries(roles)) {
            const variable = set.scalarVars[Number(register)];
            const high = resource(set.buffers[highSlot]);
            const low = resource(set.buffers[lowSlot]);
            if (!variable || !menuVars.has(variable) || !high.filename || !low.filename) {
                continue;
            }
            found.push({
                kind: "shape_slider",
                mode: "midpoint_pair",
                name: variable,
                var: `${prefix}${variable}`,
                min: 0,
                max: 1,
                step: 0.01,
                baseFile: base.filename,
                lowFile: low.filename,
                targetFile: high.filename,
                stride: base.stride ?? 40,
                source,
                iniPath: set.lines[0]?.iniPath,
                section: set.lines[0]?.section ?? "",
            });
        }
    }

    return found;
}
