import { Manager as ModRuntime } from "@bindings/modruntime";
import { Tools } from "@bindings/tools";
import type { Inspection, Look } from "@bindings/tools/liquid_emitter";
import type { ModelViewerDialogSource } from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import { normalizeModelViewerTransport } from "@renderer/components/tools/model-viewer/model-viewer-transport";
import { modRuntimesQueryKey } from "@renderer/components/xxmi/xxmi-mod-runtimes";
import { Logger } from "@renderer/lib/logger";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

export const IN_PLACE = "in_place";
export const NEW_MOD = "new_mod";
export type ApplyMode = typeof IN_PLACE | typeof NEW_MOD;
export const RUNTIME_NAME = "LiquidGravityProbe";
export const SLOT_COUNT_SETTING = "slotCount";

export function liquidEmitterQueryKey(modPath: string) {
    return ["tools:liquidEmitter", modPath];
}

/** Loads the mod into the model viewer and tracks its emitters and runtime. */
export function useLiquidEmitterSession(modPath: string) {
    const queryClient = useQueryClient();
    const [source, setSource] = useState<ModelViewerDialogSource | null>(null);
    const [loadError, setLoadError] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        let sessionId: string | null = null;
        Tools.LoadModViewer(modPath)
            .then((result) => {
                sessionId = result.memorySessionId;
                if (cancelled) {
                    void Tools.CleanupModelViewer(sessionId);
                    return;
                }
                setSource({
                    mode: "payload",
                    transport: normalizeModelViewerTransport(result),
                    memorySessionId: result.memorySessionId,
                    modPath,
                    name: result.name,
                });
            })
            .catch((error: unknown) => {
                if (cancelled) return;
                Logger.capture("liquid-emitter:load", error);
                setLoadError(toErrorMessage(error));
            });
        return () => {
            cancelled = true;
            if (sessionId) void Tools.CleanupModelViewer(sessionId);
        };
    }, [modPath]);

    const inspection = useQuery({
        queryKey: liquidEmitterQueryKey(modPath),
        queryFn: () => Tools.LiquidEmitterInspect(modPath),
        gcTime: 0,
        retry: false,
    });

    const onSuccess = (result: Inspection) =>
        queryClient.setQueryData(liquidEmitterQueryKey(modPath), result);
    // A new-mod apply and a restore answer for another folder, which the tool
    // then opens; result.modPath is how the backend spells that folder.
    const onMoved = (result: Inspection) =>
        queryClient.setQueryData(liquidEmitterQueryKey(result.modPath), result);

    const install = useMutation({
        mutationFn: () => Tools.LiquidEmitterInstallRuntime(modPath),
        onSuccess,
    });
    const apply = useMutation({
        mutationFn: Tools.LiquidEmitterApply,
        onSuccess: (result, input) =>
            input.mode === NEW_MOD ? onMoved(result) : onSuccess(result),
    });
    const remove = useMutation({
        mutationFn: (pids: number[]) => Tools.LiquidEmitterRemove({ modPath, pids }),
        onSuccess,
    });
    const setLook = useMutation({
        mutationFn: ({ pid, look }: { pid: number; look: Look }) =>
            Tools.LiquidEmitterSetLook({ modPath, pid, look }),
        onSuccess,
    });
    // The manager writes every setting it is given and resets the rest, so
    // the others go along unchanged.
    const raiseSlotCount = useMutation({
        mutationFn: ({ inspection, slotCount }: { inspection: Inspection; slotCount: number }) =>
            ModRuntime.SetSettings(inspection.importer, RUNTIME_NAME, {
                ...Object.fromEntries(
                    (inspection.runtime.settings ?? []).map((setting) => [
                        setting.id,
                        setting.value,
                    ]),
                ),
                [SLOT_COUNT_SETTING]: String(slotCount),
            }),
        onSuccess: (runtime, { inspection }) => {
            queryClient.setQueryData(liquidEmitterQueryKey(modPath), { ...inspection, runtime });
            void queryClient.invalidateQueries({
                queryKey: modRuntimesQueryKey(inspection.importer),
            });
        },
    });
    const restore = useMutation({
        mutationFn: () => Tools.LiquidEmitterRestoreSource(modPath),
        onSuccess: onMoved,
    });

    return {
        source,
        loadError,
        inspection,
        install,
        apply,
        remove,
        setLook,
        raiseSlotCount,
        restore,
    };
}
