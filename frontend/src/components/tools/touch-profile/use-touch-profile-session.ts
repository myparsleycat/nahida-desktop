import { Dialog, Shell } from "@bindings/platform";
import { Tools } from "@bindings/tools";
import { isCurrentRequest, resolveIfCurrent } from "@renderer/lib/generation-gate";
import type {
    TouchBoneZoneSelection,
    TouchProfileComponentSummary,
} from "@shared/touch-profile-preview";
import {
    TOUCH_PROFILE_MASK_CURVE_RANGE,
    TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE,
    TOUCH_PROFILE_MASK_STRENGTH_RANGE,
    type TouchProfileAdvancedSettings,
    type TouchZoneSettings,
} from "@shared/touch-profile-settings";
import { toErrorMessage } from "@shared/utils";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import {
    ALL_ZONES,
    DEFAULT_MASK_STRENGTH,
    DEFAULT_MASK_CURVE,
    DEFAULT_MASK_RADIUS_SCALE,
} from "./touch-profile-defaults";
import { createTouchSettingsBatch } from "./touch-profile-settings-batch";
import { useTouchProfilePreview } from "./use-touch-profile-preview";

export type TouchProfileSessionOptions = {
    fixedTargetPath?: string;
    onApplied?: (result: { outputModRoot: string; sourceModRoot: string }) => void;
    onRolledBack?: (sourceModRoot: string) => void;
    onResetSelectionPreview: () => void;
};
type TouchDraft = {
    sessionId: string;
    sourceModRoot: string;
    canAutoApply: boolean;
    warnings: string[];
    analysis: { supportGrade: string };
    llm?: {
        protocol: string;
        endpoint: string;
        model: string;
        reasoning: string;
    };
    components: TouchProfileComponentSummary[];
};
type TouchModInspection = {
    sessionId: string;
    modRoot: string;
    supportGrade: string;
    supportReasons: string[];
    components: Array<{
        id: string;
        name: string;
        kind: string;
        supportGrade: string;
        interactiveCandidate: boolean;
        vertexCount: number;
        indexCount: number;
        variantKey?: string;
        variantCondition?: string;
        hasBlend: boolean;
        bones: Array<{ id: number; vertexCount: number }>;
    }>;
};
type TouchApplyResultState = {
    sessionId: string;
    outputModRoot: string;
    sourceModRoot: string;
    reenableSourceOnRollback: boolean;
};
type TouchProfileInputError = "already_touch" | "suspected_touch";
const DEFAULT_BONE_WEIGHT_THRESHOLD = 0.01;
const DEFAULT_BONE_WEIGHT_THRESHOLD_MAX = 1;
const TOUCH_SETTINGS_DEBOUNCE_MS = 120;
export function useTouchProfileSession({
    fixedTargetPath,
    onApplied,
    onRolledBack,
    onResetSelectionPreview,
}: TouchProfileSessionOptions) {
    const { t } = useTranslation();
    const draftSessionRef = useRef<string | null>(null);
    const loadGenerationRef = useRef(0);
    const [modPath, setModPath] = useState(fixedTargetPath ?? "");
    const [loading, setLoading] = useState(false);
    const [applying, setApplying] = useState(false);
    const [rollingBack, setRollingBack] = useState(false);
    const [draft, setDraft] = useState<TouchDraft | null>(null);
    const [inspection, setInspection] = useState<TouchModInspection | null>(null);
    const [phase, setPhase] = useState<"select" | "review">("select");
    const [selectedMeshIds, setSelectedMeshIds] = useState<Set<string>>(new Set());
    const [selectedMeshId, setSelectedMeshId] = useState("");
    const [result, setResult] = useState<TouchApplyResultState | null>(null);
    const [inputError, setInputError] = useState<TouchProfileInputError | null>(null);
    const analysisMode = "bone" as const;
    const [weightThreshold, setWeightThreshold] = useState<[number, number]>([
        DEFAULT_BONE_WEIGHT_THRESHOLD,
        DEFAULT_BONE_WEIGHT_THRESHOLD_MAX,
    ]);
    const [boneZoneAssignments, setBoneZoneAssignments] = useState<
        Record<string, TouchBoneZoneSelection[]>
    >({});
    const [selectedComponentId, setSelectedComponentId] = useState("");
    const [selectedZoneId, setSelectedZoneId] = useState(ALL_ZONES);
    const [linkedComponents, setLinkedComponents] = useState<Record<string, boolean>>({});
    const [pendingSettingsSaves, setPendingSettingsSaves] = useState(0);
    const isFixedTarget = Boolean(fixedTargetPath);
    const selectedMeshComponentId = inspection?.components.find(
        (component) => component.id === selectedMeshId,
    )?.id;
    const interactiveComponents = useMemo(
        () =>
            (draft?.components ?? []).filter(
                (component) => component.interactive && component.zones.length > 0,
            ),
        [draft?.components],
    );
    const activeComponentId =
        interactiveComponents.find((component) => component.componentId === selectedComponentId)
            ?.componentId ??
        interactiveComponents[0]?.componentId ??
        "";
    const selectedComponent = interactiveComponents.find(
        (component) => component.componentId === activeComponentId,
    );
    const activeZoneId =
        selectedZoneId === ALL_ZONES ||
        selectedComponent?.zones.some((zone) => zone.id === selectedZoneId)
            ? selectedZoneId
            : ALL_ZONES;
    const selectedZone = selectedComponent?.zones.find((zone) => zone.id === activeZoneId);

    const {
        meshPreview,
        meshPreviewLoading,
        meshPreviewError,
        preview,
        lastValidPreview,
        previewLoading,
        previewError,
        previewReloadVersion,
        cancelPreview,
        clearTopology,
        resetPreview,
        resetMeshPreview,
        clearPreviewError,
        reloadPreview,
    } = useTouchProfilePreview({
        draftSessionId: draft?.sessionId,
        inspectionSessionId: inspection?.sessionId,
        activeComponentId,
        selectedMeshComponentId,
        phase,
    });
    const settingsSessionId = draft?.sessionId ?? inspection?.sessionId;
    const settingsBatch = useMemo(
        () =>
            createTouchSettingsBatch({
                debounceMs: TOUCH_SETTINGS_DEBOUNCE_MS,
                save: async (changes) => {
                    if (!settingsSessionId) throw new Error("Touch profile session is closed");
                    return Tools.TouchProfileUpdateZoneSettingsBatch({
                        sessionId: settingsSessionId,
                        changes,
                    });
                },
                onPendingChange: (pending) => setPendingSettingsSaves(pending ? 1 : 0),
                onPreviewRequired: () => reloadPreview(),
                onError: (error) => {
                    toast.error(t("page.tools.touch_profile.toast.settings_save_failed"), {
                        description: toErrorMessage(error),
                    });
                },
            }),
        [settingsSessionId, t],
    );
    const loadMod = async (pathOverride?: string) => {
        const targetPath = pathOverride ?? modPath;
        if (!targetPath) return;
        const requestId = ++loadGenerationRef.current;
        if (pathOverride && isCurrentRequest(requestId, loadGenerationRef)) {
            setModPath(pathOverride);
        }
        if (!isCurrentRequest(requestId, loadGenerationRef)) return;
        setLoading(true);
        setDraft(null);
        setResult(null);
        setInputError(null);
        resetPreview();
        resetMeshPreview();
        setSelectedComponentId("");
        setSelectedZoneId(ALL_ZONES);
        setLinkedComponents({});
        setBoneZoneAssignments({});
        try {
            const previousSession = draftSessionRef.current;
            draftSessionRef.current = null;
            if (previousSession) await Tools.TouchProfileCloseSession(previousSession);
            clearTopology();
            settingsBatch.clear();
            const next = await resolveIfCurrent(requestId, loadGenerationRef, () =>
                Tools.TouchProfilePrepare({
                    modPath: targetPath,
                }),
            );
            if (next === undefined) return;
            draftSessionRef.current = next.sessionId;
            setInspection(next as never);
            setSelectedMeshIds(
                new Set(
                    (next.components ?? [])
                        .filter((component) => component.interactiveCandidate)
                        .map((c) => c.id),
                ),
            );
            setPhase("select");
        } catch (error) {
            if (!isCurrentRequest(requestId, loadGenerationRef)) return;
            const inputErrorCode = getTouchProfileInputError(toErrorMessage(error));
            if (inputErrorCode) {
                setInputError(inputErrorCode);
                toast.error(t(`page.tools.touch_profile.input_error.${inputErrorCode}.title`), {
                    description: t(
                        `page.tools.touch_profile.input_error.${inputErrorCode}.description`,
                    ),
                });
            } else {
                toast.error(t("page.tools.touch_profile.toast.load_failed"), {
                    description: toErrorMessage(error),
                });
            }
        } finally {
            if (isCurrentRequest(requestId, loadGenerationRef)) {
                setLoading(false);
            }
        }
    };
    useEffect(() => {
        draftSessionRef.current = draft?.sessionId ?? inspection?.sessionId ?? null;
    }, [draft?.sessionId, inspection?.sessionId]);
    useEffect(() => {
        return () => {
            const sessionId = draftSessionRef.current;
            if (!sessionId) return;
            cancelPreview();
            clearTopology();
            void Tools.TouchProfileCloseSession(sessionId);
        };
    }, []);
    useEffect(() => () => settingsBatch.dispose(), [settingsBatch]);
    useEffect(() => {
        if (!fixedTargetPath) return;
        queueMicrotask(() => {
            void loadMod(fixedTargetPath);
        });
    }, [fixedTargetPath]);
    const activePreview = preview?.componentId === activeComponentId ? preview : null;
    // Keep the last successful mesh while a new component loads so the Canvas stays mounted.
    const displayPreview = activePreview ?? (previewLoading ? lastValidPreview : null);
    const selectFolder = async () => {
        const selected = await Dialog.ShowOpenDialog({
            title: "",
            defaultPath: "",
            filters: [],
            properties: ["openDirectory"],
        });
        if (selected && !selected.canceled && selected.filePaths?.[0]) {
            setModPath(selected.filePaths[0]);
        }
    };
    const analyzeSelected = async () => {
        if (!inspection || inspection.components.length === 0) return;
        if (selectedMeshIds.size === 0) {
            toast.error(t("page.tools.touch_profile.toast.no_components_selected"));
            return;
        }
        setLoading(true);
        setInputError(null);
        try {
            const boneSelections =
                analysisMode === "bone"
                    ? Object.entries(boneZoneAssignments)
                          .filter(([, zones]) => zones.length > 0)
                          .map(([componentId, zones]) => ({ componentId, zones }))
                    : undefined;
            const next = await Tools.TouchProfileAnalyzeComponents({
                sessionId: inspection.sessionId,
                componentIds: [...selectedMeshIds],
                mode: analysisMode,
                boneSelections,
                weightThreshold: analysisMode === "bone" ? weightThreshold : undefined,
            });
            setDraft(next as unknown as TouchDraft);
            setPhase("review");
            toast.success(t("page.tools.touch_profile.toast.loaded"));
        } catch (error) {
            const inputErrorCode = getTouchProfileInputError(toErrorMessage(error));
            if (inputErrorCode) {
                setInputError(inputErrorCode);
                toast.error(t(`page.tools.touch_profile.input_error.${inputErrorCode}.title`), {
                    description: t(
                        `page.tools.touch_profile.input_error.${inputErrorCode}.description`,
                    ),
                });
            } else {
                toast.error(t("page.tools.touch_profile.toast.load_failed"), {
                    description: toErrorMessage(error),
                });
            }
        } finally {
            setLoading(false);
        }
    };
    const backToSelect = () => {
        setDraft(null);
        setPhase("select");
        resetPreview();
        setSelectedComponentId("");
        setSelectedZoneId(ALL_ZONES);
        setBoneZoneAssignments({});
        onResetSelectionPreview();
    };
    const applyDraft = async (force = false) => {
        if (!draft || applying || pendingSettingsSaves > 0) return;
        const regenerating = result !== null;
        setApplying(true);
        try {
            const next = regenerating
                ? await Tools.TouchProfileRegenerate({
                      sessionId: draft.sessionId,
                      force,
                  })
                : await Tools.TouchProfileApply({
                      sessionId: draft.sessionId,
                      force,
                  });
            const applyResult = {
                sessionId: next.sessionId,
                outputModRoot: next.outputModRoot,
                sourceModRoot: next.sourceModRoot,
                reenableSourceOnRollback: next.reenableSourceOnRollback,
            };
            setResult(applyResult);
            toast.success(
                t(
                    regenerating
                        ? "page.tools.touch_profile.toast.regenerated"
                        : "page.tools.touch_profile.toast.created",
                ),
                {
                    description: next.outputModRoot,
                },
            );
            if (!regenerating) {
                onApplied?.({
                    outputModRoot: next.outputModRoot,
                    sourceModRoot: next.sourceModRoot,
                });
            }
        } catch (error) {
            toast.error(
                t(
                    regenerating
                        ? "page.tools.touch_profile.toast.regenerate_failed"
                        : "page.tools.touch_profile.toast.create_failed",
                ),
                {
                    description: toErrorMessage(error),
                },
            );
        } finally {
            setApplying(false);
        }
    };
    const updateZoneSettings = (
        componentId: string,
        zoneId: string,
        settings: TouchZoneSettings,
        options?: { refreshPreview?: boolean; zoneIds?: string[] },
    ) => {
        if (!draft || applying || rollingBack) return;
        const zoneIds = options?.zoneIds ?? [zoneId];
        if (zoneIds.length === 0) return;
        setDraft((current) => {
            if (!current) return current;
            return {
                ...current,
                components: current.components.map((component) =>
                    component.componentId !== componentId
                        ? component
                        : {
                              ...component,
                              zones: component.zones.map((zone) =>
                                  zoneIds.includes(zone.id) ? { ...zone, settings } : zone,
                              ),
                          },
                ),
            };
        });
        cancelPreview();
        settingsBatch.enqueue(
            zoneIds.map((nextZoneId) => ({
                componentId,
                zoneId: nextZoneId,
                settings,
            })),
            Boolean(options?.refreshPreview),
        );
    };
    const setComponentLinked = (componentId: string, linked: boolean) => {
        setLinkedComponents((current) => ({ ...current, [componentId]: linked }));
        if (!linked || !draft) return;
        const component = draft.components.find((item) => item.componentId === componentId);
        const sourceZone = component?.zones[0];
        if (!component || !sourceZone || component.zones.length < 2) return;
        updateZoneSettings(componentId, sourceZone.id, sourceZone.settings, {
            refreshPreview: true,
            zoneIds: component.zones.map((zone) => zone.id),
        });
    };
    const updateZoneAdvancedSetting = <K extends keyof TouchProfileAdvancedSettings>(
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        key: K,
        value: number,
    ) => {
        updateZoneSettings(componentId, zone.id, {
            ...zone.settings,
            physicsPreset: "custom",
            advanced: { ...zone.settings.advanced, [key]: value },
        });
    };
    const updateZoneMaskStrength = (
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        value: number,
        options?: { zoneIds?: string[] },
    ) => {
        if (!Number.isFinite(value)) return;
        updateZoneSettings(
            componentId,
            zone.id,
            {
                ...zone.settings,
                maskStrength: Math.max(
                    TOUCH_PROFILE_MASK_STRENGTH_RANGE.min,
                    Math.min(TOUCH_PROFILE_MASK_STRENGTH_RANGE.max, value),
                ),
            },
            { refreshPreview: true, ...options },
        );
    };
    const resetZoneMaskStrength = (
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        options?: { zoneIds?: string[] },
    ) => {
        updateZoneMaskStrength(componentId, zone, DEFAULT_MASK_STRENGTH, options);
    };
    const updateZoneMaskCurve = (
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        value: number,
        options?: { zoneIds?: string[] },
    ) => {
        if (!Number.isFinite(value)) return;
        updateZoneSettings(
            componentId,
            zone.id,
            {
                ...zone.settings,
                maskCurve: Math.max(
                    TOUCH_PROFILE_MASK_CURVE_RANGE.min,
                    Math.min(TOUCH_PROFILE_MASK_CURVE_RANGE.max, value),
                ),
            },
            { refreshPreview: true, ...options },
        );
    };
    const resetZoneMaskCurve = (
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        options?: { zoneIds?: string[] },
    ) => {
        updateZoneMaskCurve(componentId, zone, DEFAULT_MASK_CURVE, options);
    };
    const updateZoneMaskRadiusScale = (
        componentId: string,
        zone: TouchProfileComponentSummary["zones"][number],
        delta: number,
        options?: { zoneIds?: string[] },
    ) => {
        if (!Number.isFinite(delta)) return;
        const current = zone.settings.maskRadiusScale ?? DEFAULT_MASK_RADIUS_SCALE;
        const next = Math.max(
            TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.min,
            Math.min(
                TOUCH_PROFILE_MASK_RADIUS_SCALE_RANGE.max,
                Math.round((current + delta) * 100) / 100,
            ),
        );
        if (next === current) return;
        updateZoneSettings(
            componentId,
            zone.id,
            { ...zone.settings, maskRadiusScale: next },
            { refreshPreview: true, ...options },
        );
    };
    const discardDraft = async () => {
        if (!draft && !inspection) return;
        const sessionId = draft?.sessionId ?? inspection?.sessionId;
        if (sessionId) await Tools.TouchProfileCloseSession(sessionId);
        draftSessionRef.current = null;
        clearTopology();
        settingsBatch.clear();
        setDraft(null);
        setInspection(null);
        setPhase("select");
        resetPreview();
        resetMeshPreview();
        setInputError(null);
        setSelectedComponentId("");
        setSelectedZoneId(ALL_ZONES);
    };
    const openResult = async () => {
        if (!result) return;
        await Shell.OpenPath(result.outputModRoot);
    };
    const rollbackResult = async () => {
        if (!result || rollingBack) return;
        setRollingBack(true);
        try {
            const next = await Tools.TouchProfileRollback({
                sessionId: result.sessionId,
                outputModRoot: result.outputModRoot,
                sourceModRoot: result.sourceModRoot,
                reenableSourceOnRollback: result.reenableSourceOnRollback,
            });
            setResult(null);
            clearPreviewError();
            reloadPreview();
            setModPath(next.sourceModRoot);
            setDraft((current) =>
                current ? { ...current, sourceModRoot: next.sourceModRoot } : current,
            );
            toast.success(t("page.tools.touch_profile.toast.rolled_back"), {
                description: next.sourceModRoot,
            });
            onRolledBack?.(next.sourceModRoot);
        } catch (error) {
            toast.error(t("page.tools.touch_profile.toast.rollback_failed"), {
                description: toErrorMessage(error),
            });
        } finally {
            setRollingBack(false);
        }
    };
    return {
        t,
        modPath,
        setModPath,
        loading,
        applying,
        rollingBack,
        draft,
        inspection,
        phase,
        selectedMeshIds,
        setSelectedMeshIds,
        selectedMeshId,
        setSelectedMeshId,
        result,
        inputError,
        analysisMode,
        weightThreshold,
        setWeightThreshold,
        boneZoneAssignments,
        setBoneZoneAssignments,
        setSelectedComponentId,
        setSelectedZoneId,
        linkedComponents,
        pendingSettingsSaves,
        isFixedTarget,
        interactiveComponents,
        activeComponentId,
        selectedComponent,
        activeZoneId,
        selectedZone,
        activePreview,
        displayPreview,
        selectFolder,
        analyzeSelected,
        backToSelect,
        applyDraft,
        updateZoneSettings,
        setComponentLinked,
        updateZoneAdvancedSetting,
        updateZoneMaskStrength,
        resetZoneMaskStrength,
        updateZoneMaskCurve,
        resetZoneMaskCurve,
        updateZoneMaskRadiusScale,
        discardDraft,
        openResult,
        rollbackResult,
        meshPreview,
        meshPreviewLoading,
        meshPreviewError,
        previewLoading,
        previewError,
        previewReloadVersion,
    };
}
export type TouchProfileSession = ReturnType<typeof useTouchProfileSession>;
function getTouchProfileInputError(message: string): TouchProfileInputError | null {
    if (message.includes("TOUCH_PROFILE_INPUT_ALREADY_TOUCH")) return "already_touch";
    if (message.includes("TOUCH_PROFILE_INPUT_SUSPECTED_TOUCH")) return "suspected_touch";
    return null;
}
