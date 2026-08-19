import path from "node:path";

import type { ViewerStateValue, ViewerVariable, ModViewerPayload } from "@shared/mod-viewer/types";

import {
    attachShapeSliders,
    attachWwmiDumpTextures,
    buildDrawGroups,
    type DrawGroup,
} from "./draw-groups";
import {
    canonicalVarNames,
    discoverIniPaths,
    extractResources,
    iniScope,
    parseIniFile,
    rebaseResources,
} from "./ini";
import { extractMenuToggles } from "./menu";
import { buildMeshResult } from "./mesh-builder";
import { extractShapeSliders } from "./shapes";
import { extractStateRules } from "./state-rules";
import { extractToggleKeys, extractVariableDefaults } from "./toggles";

export async function loadModViewerPayload(modPath: string): Promise<ModViewerPayload> {
    const folderPath = path.resolve(modPath);
    const iniPaths = await discoverIniPaths(folderPath);
    if (iniPaths.length === 0) {
        throw new Error("No active .ini files found in this folder.");
    }

    const groups: DrawGroup[] = [];
    const toggleKeys: ReturnType<typeof extractToggleKeys> = {};
    const menuSlots: ReturnType<typeof extractMenuToggles> = {};
    const defaults: Record<string, string> = {};
    const stateRules: ReturnType<typeof extractStateRules> = [];
    const shapeSliders: ReturnType<typeof extractShapeSliders> = [];
    const seenLabels: Record<string, number> = {};
    const multi = iniPaths.length > 1;

    for (const iniPath of iniPaths) {
        const sections = await parseIniFile(iniPath);
        const { prefix, source } = iniScope(iniPath, folderPath, multi);
        const resources = rebaseResources(extractResources(sections), iniPath, folderPath);
        const canonicalVars = canonicalVarNames(sections);
        const toggles = extractToggleKeys(sections, prefix, source, canonicalVars);
        const menu = extractMenuToggles(sections, prefix, source, canonicalVars);
        const rules = extractStateRules(sections, prefix, canonicalVars);
        const shapes = extractShapeSliders(sections, resources, prefix, source, canonicalVars);
        const variableDefaults = extractVariableDefaults(sections, prefix, canonicalVars);

        const gatingVars = new Set<string>([
            ...Object.values(toggles).flatMap((info) => Object.keys(info.vars)),
            ...Object.values(menu).map((info) => info.var),
            ...Object.values(menu).flatMap((info) => info.effects.map((effect) => effect.var)),
            ...rules.map((rule) => rule.var),
        ]);
        const scanPrefix = prefix ?? "";
        const scanGatingVars = new Set(
            [...gatingVars].map((value) =>
                scanPrefix && value.startsWith(scanPrefix) ? value.slice(scanPrefix.length) : value,
            ),
        );
        const iniGroups = buildDrawGroups(
            sections,
            resources,
            prefix,
            source,
            seenLabels,
            scanGatingVars,
        );
        await attachWwmiDumpTextures(iniGroups, resources, folderPath);
        attachShapeSliders(iniGroups, shapes);
        groups.push(...iniGroups);
        Object.assign(toggleKeys, toggles);
        Object.assign(menuSlots, menu);
        stateRules.push(...rules);
        shapeSliders.push(...shapes);
        for (const [variable, value] of Object.entries(variableDefaults)) {
            defaults[variable] ??= value;
        }
    }

    if (groups.length === 0) {
        throw new Error(`No mesh geometry found across ${iniPaths.length} ini file(s).`);
    }

    const built = await buildMeshResult(groups, folderPath);
    if (built.meshes.length === 0) {
        throw new Error("No mesh data could be extracted (buffer files missing?).");
    }

    const gating = gatingVarsFromMeshes(built.meshes);
    const variables = buildVariables(toggleKeys, menuSlots, shapeSliders, defaults, gating);
    const defaultState: Record<string, ViewerStateValue> = { ...defaults };
    for (const variable of variables) {
        defaultState[variable.id] ??= variable.defaultValue;
    }
    for (const rule of stateRules) {
        defaultState[rule.var] ??= rule.value;
    }

    return {
        iniPath: iniPaths[0],
        modDir: folderPath,
        meshes: built.meshes,
        textures: built.textures,
        variables,
        defaultState,
        stateRules,
        uiAssets: {},
    };
}

function gatingVarsFromMeshes(meshes: ModViewerPayload["meshes"]): Set<string> {
    const found = new Set<string>();
    for (const mesh of meshes) {
        for (const group of mesh.conditions) {
            for (const clause of group) {
                found.add(clause.var);
            }
        }
        for (const field of [
            mesh.textureVariants,
            mesh.normalMapVariants,
            mesh.lightMapVariants,
            mesh.materialMapVariants,
        ]) {
            for (const variant of field) {
                for (const group of variant.conditions) {
                    for (const clause of group) {
                        found.add(clause.var);
                    }
                }
            }
        }
    }
    return found;
}

function buildVariables(
    toggleKeys: ReturnType<typeof extractToggleKeys>,
    menuSlots: ReturnType<typeof extractMenuToggles>,
    shapeSliders: ReturnType<typeof extractShapeSliders>,
    defaults: Record<string, string>,
    gating: Set<string>,
): ViewerVariable[] {
    const variables: ViewerVariable[] = [];
    const seen = new Set<string>();
    let order = 0;

    const add = (variable: ViewerVariable) => {
        if (seen.has(variable.id.toLowerCase())) {
            return;
        }
        seen.add(variable.id.toLowerCase());
        variables.push({ ...variable, order });
        order += 1;
    };

    for (const slider of shapeSliders) {
        add({
            id: slider.var,
            label: slider.name,
            defaultValue: defaults[slider.var] ?? "0",
            values: [],
            order,
            controlType: "slider",
            slider: { min: slider.min, max: slider.max, step: slider.step },
        });
    }

    for (const slot of Object.values(menuSlots)) {
        add({
            id: slot.var,
            label: slot.name,
            defaultValue: defaults[slot.var] ?? slot.values[0],
            values: slot.values.map((value) => ({ value, label: value })),
            order,
            slot: slot.slot,
            controlType: "buttons",
            effects: slot.effects,
        });
    }

    for (const info of Object.values(toggleKeys)) {
        for (const [variable, values] of Object.entries(info.vars)) {
            if (!gating.has(variable)) {
                continue;
            }
            add({
                id: variable,
                label: info.name || variable,
                defaultValue: defaults[variable] ?? values[0],
                values: values.map((value) => ({ value, label: value })),
                order,
                controlType: "buttons",
            });
        }
    }

    return variables;
}
