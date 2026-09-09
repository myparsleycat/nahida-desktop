import {
    Tools,
    type ModelViewerBlockingVariable,
    type ModelViewerResolutionSuggestion,
} from "@bindings/tools";
import { Logger } from "@renderer/lib/logger";
import type { ViewerStateValue } from "@shared/mod-viewer/types";
import { useEffect, useState } from "react";

export type IneffectiveSuggestion = Omit<ModelViewerResolutionSuggestion, "changes"> & {
    changes: NonNullable<ModelViewerResolutionSuggestion["changes"]>;
};
export type IneffectiveMap = Map<
    string,
    Map<
        string,
        {
            blockingVars: ModelViewerBlockingVariable[];
            suggestions: IneffectiveSuggestion[];
        }
    >
>;

const empty: IneffectiveMap = new Map();

export function useModelViewerIneffectiveValues(
    sessionId: string | undefined,
    state: Record<string, ViewerStateValue>,
): IneffectiveMap {
    const [result, setResult] = useState<{
        sessionId: string;
        state: typeof state;
        values: IneffectiveMap;
    }>();
    useEffect(() => {
        if (!sessionId) return;
        let ignore = false;
        const request = Tools.GetModelViewerIneffectiveValues(sessionId, state);
        void request
            .then((entries) => {
                if (ignore) return;
                const values: IneffectiveMap = new Map();
                for (const entry of entries ?? []) {
                    const variable = values.get(entry.variableId) ?? new Map();
                    variable.set(entry.value, {
                        blockingVars: entry.blockingVars ?? [],
                        suggestions: (entry.suggestions ?? []).map((suggestion) => ({
                            ...suggestion,
                            changes: suggestion.changes ?? [],
                        })),
                    });
                    values.set(entry.variableId, variable);
                }
                setResult({ sessionId, state, values });
            })
            .catch((error: unknown) => {
                if (!ignore) Logger.capture("model-viewer:ineffective-values", error);
            });
        return () => {
            ignore = true;
            void request.cancel();
        };
    }, [sessionId, state]);
    return result && result.sessionId === sessionId && result.state === state
        ? result.values
        : empty;
}
