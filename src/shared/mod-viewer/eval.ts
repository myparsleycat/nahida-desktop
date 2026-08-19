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

export function dnfSatisfied(
    condGroups: Dnf | undefined,
    state: Record<string, ViewerStateValue>,
): boolean {
    if (!condGroups || condGroups.length === 0) {
        return true;
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
        meshes: payload.meshes.map((mesh) => ({
            id: mesh.id,
            visible: dnfSatisfied(mesh.conditions, resolved),
            texKey: resolveTextureVariant(mesh.textureVariants, mesh.texKey, resolved),
            normalMapKey: resolveTextureVariant(
                mesh.normalMapVariants,
                mesh.normalMapKey,
                resolved,
            ),
            lightMapKey: resolveTextureVariant(mesh.lightMapVariants, mesh.lightMapKey, resolved),
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
        })),
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
