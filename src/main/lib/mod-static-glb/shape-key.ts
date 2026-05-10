import path from "node:path";
import * as overrideAnalysis from "./override-analysis";
import { parseMihoyoBufferGroupResourceName, parseWwmiBufferResourceName } from "./resource-loader";
import { normalizeKey } from "./shared";
import type {
    IniSection,
    Resource,
    StaticGlbRealtimeShapeKey,
    StaticGlbRealtimeShapeKeyDimension,
} from "./types";

export function collectRealtimeShapeKeys(
    sections: IniSection[],
    resources: Resource[],
    modDir: string,
): StaticGlbRealtimeShapeKey[] {
    const sectionByFullName = new Map(
        sections.map((section) => [
            normalizeKey(overrideAnalysis.getSectionFullName(section)),
            section,
        ]),
    );
    const resourceMap = new Map(
        resources.map((resource) => [normalizeKey(resource.name), resource]),
    );
    const customShaders = sections.filter(
        (section) =>
            section.header === "CustomShader" &&
            section.values.cs &&
            path.basename(section.values.cs).toLowerCase() === "shapekey.hlsl",
    );
    const shapeKeys: StaticGlbRealtimeShapeKey[] = [];

    for (const shaderSection of customShaders) {
        const shaderSectionName = overrideAnalysis.getSectionFullName(shaderSection);
        const outputEntry = shaderSection.lines
            .map((line) => line.match(/^([^=]+?)\s*=\s*copy\s+ref\s+cs-u5\s*$/i))
            .find((match): match is RegExpMatchArray => match !== null);
        const baseEntry = shaderSection.lines
            .map((line) => line.match(/^cs-u5\s*=\s*copy\s+(.+)$/i))
            .find((match): match is RegExpMatchArray => match !== null);
        if (!outputEntry || !baseEntry) {
            continue;
        }

        const outputResource = resourceMap.get(
            normalizeKey(overrideAnalysis.trimResourcePrefix(outputEntry[1].trim())),
        );
        const baseResource = resourceMap.get(
            normalizeKey(overrideAnalysis.trimResourcePrefix(baseEntry[1].trim())),
        );
        if (!outputResource || !baseResource?.filename) {
            continue;
        }

        const targetMeshPrefix = deriveBufferGroupKey(outputResource.name);
        if (!targetMeshPrefix) {
            continue;
        }

        const callers = sections.filter((section) =>
            section.lines.some(
                (line) => normalizeKey(line) === normalizeKey(`run = ${shaderSectionName}`),
            ),
        );
        const dimensions = new Map<string, StaticGlbRealtimeShapeKeyDimension>();

        for (const caller of callers) {
            const assignments = overrideAnalysis.resolveAssignmentFromSection(
                caller,
                ["x88", "x89", "cs-t51", "cs-t52", "cs-t53", "cs-t54"],
                sectionByFullName,
                new Map(),
            );
            const bottomVariableId = parseVariableToken(assignments.get(normalizeKey("x88")));
            const chestVariableId = parseVariableToken(assignments.get(normalizeKey("x89")));
            const smallerBottom = resourceMap.get(
                normalizeKey(
                    overrideAnalysis.trimResourcePrefix(
                        stripCopyPrefix(assignments.get(normalizeKey("cs-t52"))),
                    ),
                ),
            );
            const biggerBottom = resourceMap.get(
                normalizeKey(
                    overrideAnalysis.trimResourcePrefix(
                        stripCopyPrefix(assignments.get(normalizeKey("cs-t51"))),
                    ),
                ),
            );
            const smallerChest = resourceMap.get(
                normalizeKey(
                    overrideAnalysis.trimResourcePrefix(
                        stripCopyPrefix(assignments.get(normalizeKey("cs-t54"))),
                    ),
                ),
            );
            const biggerChest = resourceMap.get(
                normalizeKey(
                    overrideAnalysis.trimResourcePrefix(
                        stripCopyPrefix(assignments.get(normalizeKey("cs-t53"))),
                    ),
                ),
            );

            if (bottomVariableId && smallerBottom?.filename && biggerBottom?.filename) {
                dimensions.set(bottomVariableId, {
                    variableId: bottomVariableId,
                    smallerPath: path.resolve(modDir, smallerBottom.filename),
                    biggerPath: path.resolve(modDir, biggerBottom.filename),
                });
            }

            if (chestVariableId && smallerChest?.filename && biggerChest?.filename) {
                dimensions.set(chestVariableId, {
                    variableId: chestVariableId,
                    smallerPath: path.resolve(modDir, smallerChest.filename),
                    biggerPath: path.resolve(modDir, biggerChest.filename),
                });
            }
        }

        if (dimensions.size === 0) {
            continue;
        }

        shapeKeys.push({
            shaderPath: path.resolve(modDir, shaderSection.values.cs),
            targetMeshPrefixes: [targetMeshPrefix],
            basePath: path.resolve(modDir, baseResource.filename),
            vertexStride: baseResource.stride ?? 40,
            positionOffset: 0,
            normalOffset: 12,
            tangentOffset: 24,
            dimensions: Array.from(dimensions.values()),
        });
    }

    return shapeKeys;
}

export function deriveBufferGroupKey(resourceName: string): string | undefined {
    return (
        parseMihoyoBufferGroupResourceName(resourceName)?.key ||
        parseWwmiBufferResourceName(resourceName)?.key
    );
}

function stripCopyPrefix(value: string | undefined): string {
    return (
        value
            ?.replace(/^copy\s+/i, "")
            .replace(/^ref\s+/i, "")
            .trim() ?? ""
    );
}

export function parseVariableToken(value: string | undefined): string | undefined {
    const match = value?.match(/\$([\w.]+)/);
    return match ? normalizeKey(match[1]) : undefined;
}
