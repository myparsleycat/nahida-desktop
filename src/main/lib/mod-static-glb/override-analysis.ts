import { mergeDrawIndices } from "@native/static-glb";
import { evaluateIniCondition, evaluateIniNumericExpression } from "./ini-expression";
import { normalizeKey } from "./shared";
import type {
    DrawInstruction,
    IniBranchFrame,
    IniConditionClause,
    IniSection,
    TextureBinding,
    TextureOverrideBinding,
    VariableStateMap,
} from "./types";

export function collectTextureBindings(
    sections: IniSection[],
    sectionByFullName: Map<string, IniSection>,
    resolvedVariables: Map<string, number | string>,
): TextureBinding[] {
    const bindings: TextureBinding[] = [];
    const overrideTextureResources = sections
        .filter(
            (section) => section.header === "TextureOverride" || section.header === "CommandList",
        )
        .map((section) => {
            const resourceName = resolveSectionResourceName(
                section,
                sectionByFullName,
                resolvedVariables,
            );
            if (!resourceName) return null;
            return {
                sectionName: section.name,
                resourceName,
            };
        })
        .filter((entry): entry is { sectionName: string; resourceName: string } => !!entry);

    for (const section of sections) {
        if (section.header !== "TextureOverride") continue;

        const assignments = resolveAssignmentFromSection(
            section,
            [
                "ib",
                "this",
                "run",
                "ps-t0",
                "ps-t1",
                "ps-t2",
                "ps-t3",
                "ps-t4",
                "ps-t5",
                "ps-t6",
                "ps-t7",
                "ps-t8",
                "ps-t9",
                "ps-t10",
                "Resource\\ZZMI\\Diffuse",
            ],
            sectionByFullName,
            resolvedVariables,
        );
        const ibValues = collectSectionIbResourceNames(section);
        const resolvedIbValue = assignments.get("ib") || section.values.ib;
        if (ibValues.length === 0 && resolvedIbValue) {
            ibValues.push(trimResourcePrefix(resolvedIbValue));
        }
        if (ibValues.length === 0) continue;

        const textureResourceNames = Array.from(assignments.entries())
            .filter(([key, value]) => {
                if (!value) return false;
                const lowerKey = key.toLowerCase();
                const lowerValue = value.toLowerCase();
                return (
                    (/^pst\d+$/.test(lowerKey) ||
                        lowerKey === normalizeKey("Resource\\ZZMI\\Diffuse")) &&
                    (lowerValue.startsWith("resource") || lowerValue.startsWith("ref resource"))
                );
            })
            .map(([, value]) =>
                resolveTextureResourceReference(
                    trimResourcePrefix(value.replace(/^ref\s+/i, "")),
                    section,
                    sectionByFullName,
                    resolvedVariables,
                ),
            )
            .filter((name): name is string => !!name);

        for (const ibValue of ibValues) {
            const diffuseResourceName = resolveTextureResourceReference(
                textureResourceNames.find((name) => name.toLowerCase().includes("diffuse")) ||
                    textureResourceNames.find((name) => {
                        const lower = name.toLowerCase();
                        return !lower.includes("normal") && !lower.includes("light");
                    }) ||
                    resolveSectionResourceName(section, sectionByFullName, resolvedVariables) ||
                    resolveOverrideDiffuseResource(
                        section.name,
                        trimResourcePrefix(ibValue),
                        overrideTextureResources,
                    ),
                section,
                sectionByFullName,
                resolvedVariables,
            );

            bindings.push({
                ibResourceName: trimResourcePrefix(ibValue),
                diffuseResourceName,
                textureResourceNames,
                overrideHash: section.values.hash?.trim(),
            });
        }
    }
    return bindings;
}

export function collectTextureOverrideDrawBindings(
    sections: IniSection[],
    runtimeVariables?: Map<string, number | string>,
): TextureOverrideBinding[] {
    const variables = collectDefaultIniVariables(sections);
    if (runtimeVariables) {
        for (const [key, value] of runtimeVariables) {
            variables.set(normalizeKey(key), value);
        }
    }
    const sectionByFullName = new Map(
        sections.map((section) => [normalizeKey(getSectionFullName(section)), section]),
    );

    return sections
        .filter((section) => section.header === "TextureOverride")
        .flatMap((section) => {
            const draws = collectSectionDrawInstructions(section, variables, sectionByFullName);
            const byIb = new Map<string, DrawInstruction[]>();
            for (const draw of draws) {
                if (!draw.ibResourceName) continue;
                const key = normalizeKey(draw.ibResourceName);
                const group = byIb.get(key) ?? [];
                group.push(draw);
                byIb.set(key, group);
            }

            return Array.from(byIb.values()).map((group) => ({
                sectionName: section.name,
                ibResourceName: group[0].ibResourceName!,
                diffuseResourceName: undefined,
                overrideHash: section.values.hash?.trim(),
                draws: group,
            }));
        });
}

export function buildIndicesForState(
    bindings: TextureOverrideBinding[],
    indices: Uint32Array,
    variables: Map<string, number | string>,
    warn: (message: string) => void,
): Uint32Array {
    const activeDraws = bindings.flatMap((binding) =>
        binding.draws.filter(
            (draw) =>
                !draw.condition ||
                draw.condition.every(
                    (clause) =>
                        evaluateIniCondition(clause.expression, variables, normalizeKey) ===
                        clause.expected,
                ),
        ),
    );

    if (activeDraws.length === 0) {
        return indices;
    }

    const result = mergeDrawIndices(uint32ArrayToBuffer(indices), activeDraws);
    for (const message of result.invalidRanges) {
        warn(message);
    }
    return bufferToUint32Array(result.indices);
}

export function getSectionFullName(section: IniSection): string {
    return `${section.header}${section.name}`;
}

export function collectDefaultIniVariables(sections: IniSection[]): Map<string, number | string> {
    const variables = new Map<string, number | string>();
    for (const section of sections) {
        if (section.header !== "Constants") continue;
        for (const line of section.lines) {
            const match = line.match(
                /^(?:global|local)(?:\s+persist)?\s+([$\w\\.\\]+)(?:\s*=\s*(.+))?$/i,
            );
            if (!match) continue;
            variables.set(normalizeKey(match[1]), parseIniScalar(match[2]));
        }
    }
    return variables;
}

export function resolveSectionResourceName(
    section: IniSection,
    sectionByFullName: Map<string, IniSection>,
    defaultVariables: Map<string, number | string>,
    visited = new Set<string>(),
): string | undefined {
    const assignment = resolveAssignmentFromSection(
        section,
        ["this", "resource\\zzmi\\diffuse"],
        sectionByFullName,
        defaultVariables,
        visited,
    );
    const directThis =
        assignment.get("this") || assignment.get(normalizeKey("Resource\\ZZMI\\Diffuse"));
    return directThis?.toLowerCase().includes("resource")
        ? trimResourcePrefix(directThis.replace(/^ref\s+/i, ""))
        : undefined;
}

export function resolveTextureResourceReference(
    resourceName: string | undefined,
    section: IniSection,
    sectionByFullName: Map<string, IniSection>,
    variables: Map<string, number | string>,
    visited = new Set<string>(),
): string | undefined {
    if (!resourceName) {
        return undefined;
    }

    const normalizedName = normalizeKey(resourceName);
    if (!normalizedName || visited.has(normalizedName)) {
        return resourceName;
    }
    visited.add(normalizedName);

    const lookupKeys = buildResourceAssignmentLookupKeys(resourceName);
    const assignments = resolveAssignmentFromSection(
        section,
        lookupKeys,
        sectionByFullName,
        variables,
    );
    const nextValue = lookupKeys
        .map((key) => assignments.get(normalizeKey(key)))
        .find((value) => !!value);
    if (!nextValue) {
        return resourceName;
    }

    const nextResourceName = trimResourcePrefix(nextValue.replace(/^ref\s+/i, ""));
    if (!nextResourceName || normalizeKey(nextResourceName) === normalizedName) {
        return resourceName;
    }

    return resolveTextureResourceReference(
        nextResourceName,
        section,
        sectionByFullName,
        variables,
        visited,
    );
}

export function resolveAssignmentFromSection(
    section: IniSection,
    targetKeys: string[],
    sectionByFullName: Map<string, IniSection>,
    variables: Map<string, number | string>,
    visited = new Set<string>(),
): Map<string, string> {
    const wanted = new Set(targetKeys.map(normalizeKey));
    const normalizedName = normalizeKey(getSectionFullName(section));
    if (visited.has(normalizedName)) return new Map<string, string>();
    visited.add(normalizedName);

    const branchActive: boolean[] = [];
    const branchMatched: boolean[] = [];
    const isActive = () => branchActive.every(Boolean);
    const assignments = new Map<string, string>();

    for (const line of section.lines) {
        const trimmed = line.trim();
        const lower = trimmed.toLowerCase();

        if (lower.startsWith("if ")) {
            const parentActive = isActive();
            const matched = parentActive
                ? evaluateIniCondition(trimmed.slice(3), variables, normalizeKey)
                : false;
            branchActive.push(matched);
            branchMatched.push(matched);
            continue;
        }

        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            if (branchActive.length === 0) continue;
            const depth = branchActive.length - 1;
            const parentActive = branchActive.slice(0, depth).every(Boolean);
            const expression = lower.startsWith("elif ") ? trimmed.slice(5) : trimmed.slice(8);
            const matched =
                parentActive && !branchMatched[depth]
                    ? evaluateIniCondition(expression, variables, normalizeKey)
                    : false;
            branchActive[depth] = matched;
            branchMatched[depth] = branchMatched[depth] || matched;
            continue;
        }

        if (lower === "else") {
            if (branchActive.length === 0) continue;
            const depth = branchActive.length - 1;
            const parentActive = branchActive.slice(0, depth).every(Boolean);
            branchActive[depth] = parentActive && !branchMatched[depth];
            branchMatched[depth] = true;
            continue;
        }

        if (lower === "endif") {
            branchActive.pop();
            branchMatched.pop();
            continue;
        }

        if (!isActive()) continue;

        const runMatch = trimmed.match(/^run\s*=\s*(.+)$/i);
        if (runMatch) {
            const nestedSection = sectionByFullName.get(normalizeKey(runMatch[1].trim()));
            if (!nestedSection) {
                continue;
            }
            const nested = resolveAssignmentFromSection(
                nestedSection,
                targetKeys,
                sectionByFullName,
                variables,
                new Set(visited),
            );
            if (nested) {
                for (const [key, value] of nested) {
                    if (wanted.has(key)) {
                        assignments.set(key, value);
                    }
                }
            }
        }

        const assignmentMatch = trimmed.match(/^([^=]+?)\s*=\s*(.+)$/);
        if (!assignmentMatch) {
            continue;
        }

        const key = normalizeKey(assignmentMatch[1].trim());
        const value = assignmentMatch[2].trim();
        if (wanted.has(key)) {
            assignments.set(key, value);
        }
    }

    return assignments;
}

export function mergeVariableState(
    base: Map<string, number | string>,
    overrides?: VariableStateMap,
): Map<string, number | string> {
    const merged = new Map(base);
    if (!overrides) {
        return merged;
    }

    for (const [key, value] of Object.entries(overrides)) {
        merged.set(normalizeKey(key), value);
    }

    return merged;
}

export function trimResourcePrefix(value: string): string {
    return value
        .trim()
        .replace(/^ref\s+/i, "")
        .replace(/^Resource/i, "");
}

function collectSectionIbResourceNames(section: IniSection): string[] {
    const names = new Set<string>();
    for (const line of section.lines) {
        const match = line.trim().match(/^([^=]+?)\s*=\s*(.+)$/);
        if (!match || normalizeKey(match[1].trim()) !== "ib") continue;
        const name = trimResourcePrefix(match[2].trim());
        if (name) {
            names.add(name);
        }
    }

    if (names.size === 0 && section.values.ib) {
        names.add(trimResourcePrefix(section.values.ib));
    }

    return Array.from(names);
}

function collectSectionDrawInstructions(
    section: IniSection,
    variables: Map<string, number | string>,
    sectionByFullName: Map<string, IniSection>,
    inheritedClauses: IniConditionClause[] = [],
    inheritedIbResourceName?: string,
    visited = new Set<string>(),
): DrawInstruction[] {
    return collectSectionDrawContext(
        section,
        variables,
        sectionByFullName,
        inheritedClauses,
        inheritedIbResourceName,
        visited,
    ).instructions;
}

function collectSectionDrawContext(
    section: IniSection,
    variables: Map<string, number | string>,
    sectionByFullName: Map<string, IniSection>,
    inheritedClauses: IniConditionClause[] = [],
    inheritedIbResourceName?: string,
    visited = new Set<string>(),
): { instructions: DrawInstruction[]; currentIbResourceName?: string } {
    const instructions: DrawInstruction[] = [];
    const stack: IniBranchFrame[] = [];
    let currentIbResourceName = inheritedIbResourceName;
    const normalizedName = normalizeKey(getSectionFullName(section));
    if (visited.has(normalizedName)) {
        return { instructions, currentIbResourceName };
    }
    visited.add(normalizedName);

    for (const rawLine of section.lines) {
        const trimmed = rawLine.trim();
        const lower = trimmed.toLowerCase();

        if (lower.startsWith("if ")) {
            const expression = trimmed.slice(3).trim();
            stack.push({
                // Each frame carries the clauses required for the current branch and the
                // accumulated inverse used by later `elif` / `else` branches.
                activeClauses: [{ expression, expected: true }],
                inverseClauses: [{ expression, expected: false }],
            });
            continue;
        }

        if (lower.startsWith("elif ") || lower.startsWith("else if ")) {
            const previous = stack.pop();
            const expression = (
                lower.startsWith("elif ") ? trimmed.slice(5) : trimmed.slice(8)
            ).trim();
            stack.push({
                activeClauses: [
                    ...(previous?.inverseClauses ?? []),
                    { expression, expected: true },
                ],
                inverseClauses: [
                    ...(previous?.inverseClauses ?? []),
                    { expression, expected: false },
                ],
            });
            continue;
        }

        if (lower === "else") {
            const previous = stack.pop();
            if (!previous) continue;
            stack.push({
                activeClauses: previous.inverseClauses,
                inverseClauses: [],
            });
            continue;
        }

        if (lower === "endif") {
            stack.pop();
            continue;
        }

        const runMatch = trimmed.match(/^run\s*=\s*(.+)$/i);
        if (runMatch) {
            const nestedSection = sectionByFullName.get(normalizeKey(runMatch[1].trim()));
            if (!nestedSection) {
                continue;
            }

            const activeConditions = [
                ...inheritedClauses,
                ...stack.flatMap((entry) => entry.activeClauses),
            ];
            const nested = collectSectionDrawContext(
                nestedSection,
                variables,
                sectionByFullName,
                activeConditions,
                currentIbResourceName,
                new Set(visited),
            );
            instructions.push(...nested.instructions);
            currentIbResourceName = nested.currentIbResourceName ?? currentIbResourceName;
            continue;
        }

        const assignmentMatch = trimmed.match(/^([^=]+?)\s*=\s*(.+)$/);
        if (assignmentMatch && normalizeKey(assignmentMatch[1].trim()) === "ib") {
            currentIbResourceName = trimResourcePrefix(assignmentMatch[2].trim());
            continue;
        }

        const drawMatch = trimmed.match(
            /^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$/i,
        );
        const drawInstancedMatch = !drawMatch
            ? trimmed.match(
                  /^drawindexedinstanced\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$/i,
              )
            : null;
        if (!drawMatch && !drawInstancedMatch) continue;

        const indexCount = evaluateIniNumericExpression(
            (drawMatch ?? drawInstancedMatch)![1],
            variables,
            normalizeKey,
        );
        const startIndex = evaluateIniNumericExpression(
            drawMatch ? drawMatch[2] : drawInstancedMatch![3],
            variables,
            normalizeKey,
        );
        const baseVertex = evaluateIniNumericExpression(
            drawMatch ? drawMatch[3] : drawInstancedMatch![4],
            variables,
            normalizeKey,
        );
        if (indexCount === null || startIndex === null || baseVertex === null) {
            continue;
        }

        const activeConditions = [
            ...inheritedClauses,
            ...stack.flatMap((entry) => entry.activeClauses),
        ];
        instructions.push({
            ibResourceName: currentIbResourceName,
            indexCount,
            startIndex,
            baseVertex,
            condition: activeConditions.length > 0 ? activeConditions : undefined,
        });
    }

    return { instructions, currentIbResourceName };
}

function resolveOverrideDiffuseResource(
    sectionName: string,
    ibResourceName: string,
    overrideTextureResources: Array<{
        sectionName: string;
        resourceName: string;
    }>,
): string | undefined {
    const exactSection = `${sectionName}Diffuse`.toLowerCase();
    const ibStem = ibResourceName.replace(/IB$/i, "");
    const exactResource = `${ibStem}Diffuse`.toLowerCase();
    const familyStem = ibStem.replace(/(?<=[a-z0-9])[A-Z]$/g, "");
    const sectionAliases = buildDiffuseLookupAliases(sectionName);
    const ibAliases = buildDiffuseLookupAliases(ibStem);
    const familyAliases = buildDiffuseLookupAliases(familyStem);

    const preferred = overrideTextureResources.filter((entry) => {
        const sectionLower = entry.sectionName.toLowerCase();
        const resourceLower = entry.resourceName.toLowerCase();
        return sectionLower.includes("diffuse") || resourceLower.includes("diffuse");
    });

    return (
        preferred.find((entry) => entry.sectionName.toLowerCase() === exactSection)?.resourceName ||
        preferred.find((entry) =>
            sectionAliases.some((alias) => entry.sectionName.toLowerCase() === `${alias}diffuse`),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase() === exactResource)
            ?.resourceName ||
        preferred.find((entry) =>
            ibAliases.some((alias) => entry.resourceName.toLowerCase() === `${alias}diffuse`),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(sectionName.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            sectionAliases.some((alias) => entry.sectionName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.sectionName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            familyAliases.some((alias) => entry.sectionName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) => entry.resourceName.toLowerCase().startsWith(ibStem.toLowerCase()))
            ?.resourceName ||
        preferred.find((entry) =>
            ibAliases.some((alias) => entry.resourceName.toLowerCase().startsWith(alias)),
        )?.resourceName ||
        preferred.find((entry) =>
            entry.resourceName.toLowerCase().startsWith(familyStem.toLowerCase()),
        )?.resourceName ||
        preferred.find((entry) =>
            familyAliases.some((alias) => entry.resourceName.toLowerCase().startsWith(alias)),
        )?.resourceName
    );
}

function buildDiffuseLookupAliases(value: string): string[] {
    const aliases = new Set<string>();
    const trimmed = value.trim();
    if (!trimmed) {
        return [];
    }

    aliases.add(trimmed.toLowerCase());

    const resourceTrimmed = trimmed.replace(/^resource/i, "");
    if (resourceTrimmed && resourceTrimmed !== trimmed) {
        aliases.add(resourceTrimmed.toLowerCase());
    }

    const meshSuffixMatch = resourceTrimmed.match(
        /(head|body|dress|hair|face|weapon|glasses|cloth|skirt|shoe|arm|leg|hand|foot)[a-z0-9]*$/i,
    );
    if (meshSuffixMatch) {
        aliases.add(meshSuffixMatch[0].toLowerCase());
        const familyAlias = meshSuffixMatch[1];
        if (familyAlias) {
            aliases.add(familyAlias.toLowerCase());
        }
    }

    return Array.from(aliases).filter(Boolean);
}

export function parseIniScalar(value?: string): number | string {
    if (!value) return 0;
    const trimmed = value.trim();
    if (/^-?\d+(?:\.\d+)?$/.test(trimmed)) {
        return Number(trimmed);
    }
    return trimmed;
}

function buildResourceAssignmentLookupKeys(resourceName: string): string[] {
    const keys = new Set<string>();
    const trimmed = resourceName.trim();
    if (!trimmed) {
        return [];
    }

    keys.add(trimmed);
    if (!/^resource/i.test(trimmed)) {
        keys.add(`Resource${trimmed}`);
    }
    return Array.from(keys);
}

function uint32ArrayToBuffer(values: Uint32Array): Buffer {
    return Buffer.from(values.buffer, values.byteOffset, values.byteLength);
}

function bufferToUint32Array(buffer: Buffer): Uint32Array {
    return new Uint32Array(
        buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength),
    );
}
