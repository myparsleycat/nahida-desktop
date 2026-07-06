import path from "node:path";

import { toErrorMessage } from "@shared/utils";
import fse from "fs-extra";

import type {
    BufferGroup,
    ConvertModToGlbBufferOptions,
    ConvertModToGlbBufferResult,
    FmtLayout,
    IbResource,
    IniSection,
    Resource,
    StaticGlbBuildContext,
    TextureBinding,
    TextureOverrideBinding,
} from "./types";

import { GlbBuilder } from "./builder";
import { loadFmtForIbCached, loadIndicesForIbCached } from "./fmt-loader";
import { buildPrimitive } from "./geometry";
import { loadIniBundle } from "./ini-loader";
import { buildMaterials } from "./material";
import { bestKeyForIb, keyMatchesIb, strictKeyMatchesIb } from "./mesh-key";
import * as overrideAnalysis from "./override-analysis";
import {
    collectMihoyoBufferGroups,
    collectResources,
    collectWwmiBufferGroups,
    detectStaticGlbModLayout,
} from "./resource-loader";
import { createWarningCollector, normalizeKey } from "./shared";

export async function prepareStaticGlbBuildContext(
    options: Pick<ConvertModToGlbBufferOptions, "modPath" | "assetPath">,
    warn: (message: string) => void,
): Promise<StaticGlbBuildContext> {
    const { iniPath, sections } = await loadIniBundle(options.modPath);
    const modDir = path.dirname(iniPath);
    const defaultVariables = overrideAnalysis.collectDefaultIniVariables(sections);
    const resources = collectResources(sections);
    const layout = detectStaticGlbModLayout(sections, resources);
    const sectionByFullName = new Map(
        sections.map((section) => [
            normalizeKey(overrideAnalysis.getSectionFullName(section)),
            section,
        ]),
    );
    const bufferGroups =
        layout === "wwmi"
            ? await collectWwmiBufferGroups(modDir, resources, warn)
            : await collectMihoyoBufferGroups(modDir, resources, warn);

    const drawBindings = overrideAnalysis.collectTextureOverrideDrawBindings(sections);

    return {
        iniPath,
        sections,
        modDir,
        defaultVariables,
        resources,
        layout,
        sectionByFullName,
        bufferGroups,
        drawBindings,
        drawBindingsByIbName: groupDrawBindingsByIbName(drawBindings),
        fmtByIbKey: new Map(),
        indicesByIbKey: new Map(),
    };
}

export async function buildModGlb(
    options: ConvertModToGlbBufferOptions,
): Promise<ConvertModToGlbBufferResult> {
    const warning = createWarningCollector(options.onWarning);
    const context = await prepareStaticGlbBuildContext(options, warning.warn);
    const resolvedVariables = overrideAnalysis.mergeVariableState(
        context.defaultVariables,
        options.variableState,
    );
    const textureBindings = overrideAnalysis.collectTextureBindings(
        context.sections,
        context.sectionByFullName,
        resolvedVariables,
    );
    const ibResources = collectIbResources(
        context.sections,
        context.resources,
        context.bufferGroups,
        context.sectionByFullName,
        resolvedVariables,
        textureBindings,
        context.drawBindings,
    );

    options.logger?.debug(
        `Detected ${context.layout} layout with ${context.resources.length} resources, ${context.bufferGroups.length} buffer groups, ${ibResources.length} IB resources, ${textureBindings.length} texture bindings`,
        "StaticGLB",
    );

    if (ibResources.length === 0) {
        throw new Error(`No index buffer Resource sections were found in ${context.iniPath}`);
    }

    const builder = new GlbBuilder();
    const materialBindings = await buildMaterials(
        builder,
        options,
        context.modDir,
        options.textureCacheDir,
        context.resources,
        textureBindings,
        warning.warn,
    );

    for (const ib of ibResources) {
        const group =
            context.bufferGroups.find((candidate) => strictKeyMatchesIb(candidate.key, ib.key)) ||
            context.bufferGroups.find((candidate) => keyMatchesIb(candidate.key, ib.key));
        if (!group) {
            warning.warn(`No matching vertex buffer found for ${ib.filename}`);
            continue;
        }

        let fmt: FmtLayout;
        try {
            fmt = await loadFmtForIbCached(context, options.assetPath, ib, group.stride);
        } catch (error) {
            warning.warn(`Skipping ${ib.filename}: ${toErrorMessage(error)}`);
            continue;
        }
        const ibPath = path.resolve(context.modDir, ib.filename);
        if (!(await fse.pathExists(ibPath))) {
            warning.warn(`Missing IB file: ${ibPath}`);
            continue;
        }

        const indices = await loadIndicesForIbCached(context, ib, ib.format || fmt.indexFormat);
        if (indices.length === 0) {
            warning.warn(`Empty IB file: ${ibPath}`);
            continue;
        }
        const activeIndices = overrideAnalysis.buildIndicesForState(
            getDrawBindingsForIb(context, ib.name),
            indices,
            resolvedVariables,
            warning.warn,
        );

        const material = materialBindings.get(normalizeKey(ib.name));
        const primitive = buildPrimitive(
            builder,
            group.vbBytes,
            group.stride,
            fmt,
            activeIndices,
            {
                includeTangents: !!options.includeTangents,
                includeVertexColors: !material,
            },
            warning.warn,
        );
        if (!primitive) {
            warning.warn(`Could not build primitive for ${ib.filename}`);
            continue;
        }

        if (material) {
            primitive.material = material.materialIndex;
        }

        builder.addMesh(path.basename(ib.filename, path.extname(ib.filename)), primitive);
    }

    if (builder.meshCount() === 0) {
        throw new Error(
            "No mesh primitives were written. Check that mod resources match asset layout files.",
        );
    }

    return {
        iniPath: context.iniPath,
        glb: builder.toGlb(),
        meshCount: builder.meshCount(),
        warningCount: warning.count,
    };
}

export function collectIbResources(
    sections: IniSection[],
    resources: Resource[],
    bufferGroups: BufferGroup[],
    sectionByFullName: Map<string, IniSection>,
    resolvedVariables: Map<string, number | string>,
    textureBindings: TextureBinding[],
    drawBindings: TextureOverrideBinding[],
): IbResource[] {
    const bufferKeys = bufferGroups.map((group) => group.key);
    const bindingsByIbName = new Map<string, TextureBinding[]>();
    for (const binding of textureBindings) {
        const key = normalizeKey(binding.ibResourceName);
        const group = bindingsByIbName.get(key) ?? [];
        group.push(binding);
        bindingsByIbName.set(key, group);
    }
    const drawBindingsByIbName = new Map<string, TextureOverrideBinding[]>();
    for (const binding of drawBindings) {
        const key = normalizeKey(binding.ibResourceName);
        const group = drawBindingsByIbName.get(key) ?? [];
        group.push(binding);
        drawBindingsByIbName.set(key, group);
    }
    const referencedIbNames = new Set([
        ...bindingsByIbName.keys(),
        ...drawBindings.map((binding) => normalizeKey(binding.ibResourceName)),
    ]);
    const activeIbNames = new Set(
        sections
            .filter((section) => section.header === "TextureOverride")
            .map((section) =>
                overrideAnalysis
                    .resolveAssignmentFromSection(
                        section,
                        ["ib"],
                        sectionByFullName,
                        resolvedVariables,
                    )
                    .get("ib"),
            )
            .filter((value): value is string => !!value)
            .map(overrideAnalysis.trimResourcePrefix)
            .map(normalizeKey),
    );

    return resources
        .filter((resource) => {
            if (!resource.filename || !resource.format) return false;

            const lowerFilename = resource.filename.toLowerCase();
            if (lowerFilename.endsWith(".ib")) {
                return activeIbNames.size === 0 || activeIbNames.has(normalizeKey(resource.name));
            }

            return referencedIbNames.has(normalizeKey(resource.name));
        })
        .map((resource) => {
            const stem = path.basename(resource.filename!, path.extname(resource.filename!));
            const key = bestKeyForIb(stem, resource.name, bufferKeys);
            const bindings = bindingsByIbName.get(normalizeKey(resource.name)) ?? [];
            const linkedDrawBindings = drawBindingsByIbName.get(normalizeKey(resource.name)) ?? [];
            const overrideHashes = Array.from(
                new Set(
                    [...bindings, ...linkedDrawBindings]
                        .map((binding) => binding.overrideHash?.trim())
                        .filter((value): value is string => !!value),
                ),
            );
            return {
                name: resource.name,
                filename: resource.filename!,
                format: resource.format!,
                key,
                overrideHash: overrideHashes[0],
                overrideHashes,
            };
        });
}

export function groupDrawBindingsByIbName(
    bindings: TextureOverrideBinding[],
): Map<string, TextureOverrideBinding[]> {
    const output = new Map<string, TextureOverrideBinding[]>();
    for (const binding of bindings) {
        const key = normalizeKey(binding.ibResourceName);
        const existing = output.get(key);
        if (existing) {
            existing.push(binding);
        } else {
            output.set(key, [binding]);
        }
    }
    return output;
}

export function getDrawBindingsForIb(
    context: StaticGlbBuildContext,
    ibName: string,
): TextureOverrideBinding[] {
    return context.drawBindingsByIbName.get(normalizeKey(ibName)) ?? [];
}
