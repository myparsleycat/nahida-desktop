import { Tools } from "@bindings/tools";
import type {
    ModelViewerBlockingVariable,
    ModelViewerResolutionSuggestion,
} from "@bindings/tools/model_viewer";
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

// A realtime slider changes the state on every pointer tick; wait for the
// ticks to settle before asking the backend to re-evaluate every variable.
export const INEFFECTIVE_VALUES_DEBOUNCE_MS = 150;

export function useModelViewerIneffectiveValues(
    sessionId: string | undefined,
    state: Record<string, ViewerStateValue>,
): IneffectiveMap {
    const [result, setResult] = useState<{
        sessionId: string;
        values: IneffectiveMap;
    }>();
    useEffect(() => {
        if (!sessionId) return;
        let ignore = false;
        let request: ReturnType<typeof Tools.GetModelViewerIneffectiveValues> | undefined;
        const timer = window.setTimeout(() => {
            request = Tools.GetModelViewerIneffectiveValues(sessionId, state);
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
                    setResult({ sessionId, values });
                })
                .catch((error: unknown) => {
                    if (ignore) return;
                    Logger.capture("model-viewer:ineffective-values", error);
                    setResult(undefined);
                });
        }, INEFFECTIVE_VALUES_DEBOUNCE_MS);
        return () => {
            ignore = true;
            window.clearTimeout(timer);
            void request?.cancel();
        };
    }, [sessionId, state]);

    // Keep the previous answer visible while the next one is pending so an
    // open dropdown does not flash to "all effective" on every state change.
    return result && result.sessionId === sessionId ? result.values : empty;
}
