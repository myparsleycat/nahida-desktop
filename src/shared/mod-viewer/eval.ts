import type {
    Dnf,
    EvaluatedViewerState,
    TextureVariant,
    ViewerEvalInput,
    ViewerMenuGuard,
    ViewerStateRule,
    ViewerStateValue,
    ViewerVariable,
} from "./types";

export type IneffectiveSuggestion = {
    display: string;
    changes: {
        varId: string;
        varLabel: string;
        fromValue: string;
        toValue: string;
    }[];
};

export type BlockingVar = {
    id: string;
    label: string;
    value: ViewerStateValue;
};

export type IneffectiveEntry = {
    blockingVars: BlockingVar[];
    suggestions: IneffectiveSuggestion[];
};

export type IneffectiveMap = Map<string, Map<string, IneffectiveEntry>>;

export function dnfSatisfied(
    condGroups: Dnf | undefined,
    state: Record<string, ViewerStateValue>,
): boolean {
    if (!condGroups) {
        return true;
    }
    if (condGroups.length === 0) {
        return false;
    }

    return condGroups.some((group) =>
        group.every((clause) => {
            const current = lookupStateValue(state, clause.var);
            if (current === undefined) {
                return true;
            }
            return clause.negate
                ? String(current) !== clause.value
                : String(current) === clause.value;
        }),
    );
}

export function applyStateRules(
    state: Record<string, ViewerStateValue>,
    rules: ViewerStateRule[],
): Record<string, ViewerStateValue> {
    const next = { ...state };
    for (const rule of rules) {
        if (dnfSatisfied(rule.conditions, next)) {
            next[rule.var] = rule.value;
        }
    }
    return next;
}

export function resolveTextureVariant(
    variants: TextureVariant[] | undefined,
    fallback: string | null,
    state: Record<string, ViewerStateValue>,
): string | null {
    if (!variants?.length) {
        return fallback;
    }

    for (let index = variants.length - 1; index >= 0; index--) {
        if (dnfSatisfied(variants[index].conditions, state)) {
            return variants[index].texKey;
        }
    }
    return fallback;
}

export function resolvePositionVariantIndex(
    variants: Array<{ conditions: Dnf }> | undefined,
    state: Record<string, ViewerStateValue>,
): number | null {
    if (!variants?.length) {
        return null;
    }

    for (let index = variants.length - 1; index >= 0; index--) {
        if (dnfSatisfied(variants[index].conditions, state)) {
            return index;
        }
    }
    return null;
}

export function applyVariableSelection(
    state: Record<string, ViewerStateValue>,
    variable: Pick<ViewerVariable, "id" | "effects">,
    value: ViewerStateValue,
): Record<string, ViewerStateValue> {
    const next = { ...state, [variable.id]: value };
    for (const effect of variable.effects ?? []) {
        if (menuGuardHolds(effect.when, next)) {
            next[effect.var] = effect.value;
        }
    }
    return next;
}

export function evaluateViewerState(
    payload: ViewerEvalInput,
    state: Record<string, ViewerStateValue> = {},
): EvaluatedViewerState {
    const resolved = applyStateRules({ ...payload.defaultState, ...state }, payload.stateRules);
    return {
        state: resolved,
        meshes: payload.meshes.map((mesh) => {
            const positionVariantIndex = resolvePositionVariantIndex(
                mesh.positionVariants,
                resolved,
            );
            return {
                id: mesh.id,
                visible:
                    dnfSatisfied(mesh.conditions, resolved) &&
                    (!mesh.positionVariants?.length || positionVariantIndex !== null),
                texKey: resolveTextureVariant(mesh.textureVariants, mesh.texKey, resolved),
                normalMapKey: resolveTextureVariant(
                    mesh.normalMapVariants,
                    mesh.normalMapKey,
                    resolved,
                ),
                lightMapKey: resolveTextureVariant(
                    mesh.lightMapVariants,
                    mesh.lightMapKey,
                    resolved,
                ),
                materialMapKey: resolveTextureVariant(
                    mesh.materialMapVariants,
                    mesh.materialMapKey,
                    resolved,
                ),
                shapeWeights: Object.fromEntries(
                    mesh.shapeTargets.map((target) => [
                        target.var,
                        Number(lookupStateValue(resolved, target.var) ?? 0),
                    ]),
                ),
                positionVariantIndex,
            };
        }),
    };
}

function menuGuardHolds(
    when: ViewerMenuGuard | null | undefined,
    state: Record<string, ViewerStateValue>,
): boolean {
    if (!when) {
        return true;
    }
    const current = lookupStateValue(state, when.var);
    if (current === undefined) {
        return false;
    }
    if (when.op === "==") {
        return String(current) === when.value;
    }
    if (when.op === "!=") {
        return String(current) !== when.value;
    }
    const left = Number(current);
    const right = Number(when.value);
    if (!Number.isFinite(left) || !Number.isFinite(right)) {
        return false;
    }
    if (when.op === ">") {
        return left > right;
    }
    if (when.op === "<") {
        return left < right;
    }
    if (when.op === ">=") {
        return left >= right;
    }
    if (when.op === "<=") {
        return left <= right;
    }
    return false;
}

function lookupStateValue(
    state: Record<string, ViewerStateValue>,
    variable: string,
): ViewerStateValue | undefined {
    if (state[variable] !== undefined) {
        return state[variable];
    }

    const lowered = variable.toLowerCase();
    return Object.entries(state).find(([key]) => key.toLowerCase() === lowered)?.[1];
}

export function computeIneffectiveValues(
    payload: ViewerEvalInput & { variables: ViewerVariable[] },
    state: Record<string, ViewerStateValue>,
): IneffectiveMap {
    const resolved = applyStateRules({ ...payload.defaultState, ...state }, payload.stateRules);
    const baseline = evaluateViewerState(payload, resolved);
    const result: IneffectiveMap = new Map();

    for (const variable of payload.variables) {
        if (variable.controlType === "slider" || variable.values.length === 0) {
            continue;
        }
        const ineffective = new Map<string, IneffectiveEntry>();
        const currentValue = String(
            lookupStateValue(resolved, variable.id) ?? variable.defaultValue,
        );
        for (const entry of variable.values) {
            if (String(entry.value) === currentValue) continue;
            const nextState = applyVariableSelection(resolved, variable, entry.value);
            const nextEval = evaluateViewerState(payload, nextState);
            if (!evaluatedStatesDiffer(baseline, nextEval)) {
                const blockingVars = buildBlockingVars(payload, variable.id, resolved);
                ineffective.set(String(entry.value), {
                    blockingVars,
                    suggestions: resolveSuggestions(
                        payload,
                        variable.id,
                        String(entry.value),
                        blockingVars,
                        resolved,
                        baseline,
                    ),
                });
            }
        }
        if (ineffective.size > 0) {
            result.set(variable.id, ineffective);
        }
    }
    return result;
}

function evaluatedStatesDiffer(left: EvaluatedViewerState, right: EvaluatedViewerState): boolean {
    for (let index = 0; index < left.meshes.length; index++) {
        const leftMesh = left.meshes[index];
        const rightMesh = right.meshes[index];
        if (leftMesh.visible !== rightMesh.visible) return true;
        if (leftMesh.texKey !== rightMesh.texKey) return true;
        if (leftMesh.normalMapKey !== rightMesh.normalMapKey) return true;
        if (leftMesh.lightMapKey !== rightMesh.lightMapKey) return true;
        if (leftMesh.materialMapKey !== rightMesh.materialMapKey) return true;
        if (leftMesh.positionVariantIndex !== rightMesh.positionVariantIndex) return true;
    }
    return false;
}

// Identifies other variables whose current values co-occur with the tested variable
// in mesh conditions, producing the dead combination.
function buildBlockingVars(
    payload: ViewerEvalInput & { variables: ViewerVariable[] },
    testedVar: string,
    state: Record<string, ViewerStateValue>,
): BlockingVar[] {
    const testedLower = testedVar.toLowerCase();
    const coOccurring = new Set<string>();
    for (const mesh of payload.meshes) {
        for (const group of [
            ...mesh.conditions,
            ...mesh.positionVariants.flatMap((v) => v.conditions),
        ]) {
            const hasTested = group.some((clause) => clause.var.toLowerCase() === testedLower);
            if (!hasTested) continue;
            for (const clause of group) {
                const lower = clause.var.toLowerCase();
                if (lower === testedLower) continue;
                coOccurring.add(clause.var);
            }
        }
        for (const variants of [
            mesh.textureVariants,
            mesh.normalMapVariants,
            mesh.lightMapVariants,
            mesh.materialMapVariants,
        ]) {
            for (const variant of variants) {
                const hasTested = variant.conditions.some((group) =>
                    group.some((clause) => clause.var.toLowerCase() === testedLower),
                );
                if (!hasTested) continue;
                for (const group of variant.conditions) {
                    for (const clause of group) {
                        const lower = clause.var.toLowerCase();
                        if (lower === testedLower) continue;
                        coOccurring.add(clause.var);
                    }
                }
            }
        }
    }
    const blockingVars: BlockingVar[] = [];
    for (const variable of payload.variables) {
        if (variable.id.toLowerCase() === testedLower) continue;
        if (![...coOccurring].some((v) => v.toLowerCase() === variable.id.toLowerCase())) continue;
        const currentValue = lookupStateValue(state, variable.id);
        if (currentValue === undefined) continue;
        blockingVars.push({ id: variable.id, label: variable.label, value: currentValue });
    }
    return blockingVars;
}

// For each blocking variable, tries its other values to find ones that make the
// tested variable value effective. Returns structured suggestions with the
// full set of variable changes needed to unblock the tested value.
function resolveSuggestions(
    payload: ViewerEvalInput & { variables: ViewerVariable[] },
    testedVarId: string,
    testedValue: string,
    blockingVars: BlockingVar[],
    state: Record<string, ViewerStateValue>,
    baseline: EvaluatedViewerState,
): IneffectiveSuggestion[] {
    const suggestions: IneffectiveSuggestion[] = [];
    const testedVar = payload.variables.find((v) => v.id === testedVarId);
    if (!testedVar) return suggestions;

    for (const { id: blockingVarId, label: blockingLabel, value: currentValue } of blockingVars) {
        const blockingVar = payload.variables.find((v) => v.id === blockingVarId);
        if (!blockingVar) continue;
        const currentStr = String(currentValue);
        for (const altEntry of blockingVar.values) {
            if (String(altEntry.value) === currentStr) continue;
            const altState = applyVariableSelection(state, blockingVar, altEntry.value);
            const altResolved = applyStateRules(
                { ...payload.defaultState, ...altState },
                payload.stateRules,
            );
            const testState = applyVariableSelection(altResolved, testedVar, testedValue);
            const testEval = evaluateViewerState(payload, testState);
            const altBaseline = evaluateViewerState(payload, altResolved);
            if (
                evaluatedStatesDiffer(altBaseline, testEval) &&
                evaluatedStatesDiffer(baseline, testEval)
            ) {
                const changes: IneffectiveSuggestion["changes"] = [];
                for (const variable of payload.variables) {
                    const fromValue = String(
                        lookupStateValue(state, variable.id) ?? variable.defaultValue,
                    );
                    const toValue = String(
                        lookupStateValue(testState, variable.id) ?? variable.defaultValue,
                    );
                    if (toValue !== fromValue) {
                        changes.push({
                            varId: variable.id,
                            varLabel: variable.label,
                            fromValue,
                            toValue,
                        });
                    }
                }
                suggestions.push({
                    display: `${blockingLabel}: ${currentStr} → ${altEntry.value}`,
                    changes,
                });
            }
        }
    }
    return suggestions;
}
