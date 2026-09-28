import { Auth } from "@bindings/auth";
import type { UpdateStatus } from "@bindings/xxmi";
import { Logger } from "@renderer/lib/logger";
import type { BackendStatus } from "@shared/backend";
import type { DownloadSource } from "@shared/mod";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { globalStore, useGlobalStore } from "../store/global";

export function useGlobalEvents(
    onPathSelectorModeSelect?: (data: {
        selectionId: string;
        suggestedName?: string;
        suggestedNames?: string[];
        downloadTargetName?: string;
        downloadImporterKey?: string;
        downloadSource: DownloadSource;
    }) => void,
) {
    const navi = useNavigate();
    const queryClient = useQueryClient();
    const setSession = useGlobalStore((state) => state.setSession);
    const setHasToken = useGlobalStore((state) => state.setHasToken);
    const setBackendStatus = useGlobalStore((state) => state.setBackendStatus);
    const setPendingSessionRestore = useGlobalStore((state) => state.setPendingSessionRestore);
    const { i18n } = useTranslation();

    // biome-ignore lint/correctness/useExhaustiveDependencies: <>
    useEffect(() => {
        const removeToastListener = Events.On("fn:toast", (event) => {
            const payload = event.data as
                | string
                | [string, { description?: string } | undefined]
                | undefined;
            if (Array.isArray(payload)) {
                const [message, args] = payload;
                toast(message, { description: args?.description });
                return;
            }
            toast(typeof payload === "string" ? payload : "");
        });

        const removeNaviListener = Events.On("fn:navi", (event) => {
            void navi({ to: event.data as string });
        });

        const removePathSelectorListener = Events.On("pathSelector:modeSelect", (event) => {
            if (!onPathSelectorModeSelect) return;
            const payload = Array.isArray(event.data) ? event.data[0] : event.data;
            if (payload) {
                onPathSelectorModeSelect(payload);
            }
        });

        const removeAuthListener = Events.On("auth:update", (event) => {
            const session = event.data;
            setSession(session);
            setHasToken(!!session);
        });

        const removeBackendStatusListener = Events.On("backend:status", (event) => {
            const status = event.data as BackendStatus;
            const previousStatus = globalStore.getState().backendStatus;
            const isColdStartRestore = status === "online" && previousStatus === "unknown";
            if (isColdStartRestore) {
                globalStore.setState({
                    backendStatus: status,
                    pendingSessionRestore: true,
                });
            } else {
                setBackendStatus(status);
            }
            if (status !== "online") return;
            if (
                previousStatus !== "offline" &&
                previousStatus !== "maintenance" &&
                !isColdStartRestore
            )
                return;

            void (async () => {
                try {
                    if (isColdStartRestore) {
                        await whenSessionInitialized();
                        const state = globalStore.getState();
                        if (state.session || !state.hasToken) return;
                    }

                    const session = await Auth.GetSession();
                    setSession(session);
                    setHasToken(!!session || (await Auth.HasToken()));
                } catch (error) {
                    Logger.capture(
                        "hooks/use-global-events.ts",
                        "Failed to refresh session after backend recovery",
                        error,
                    );
                } finally {
                    if (isColdStartRestore) setPendingSessionRestore(false);
                }
            })();
        });

        const removeLanguageListener = Events.On("language:update", (event) => {
            void i18n.changeLanguage(event.data as string);
        });

        const removeXXMIUpdatesListener = Events.On("xxmi:updates", (event) => {
            const payload =
                Array.isArray(event.data) && event.data.length === 1 && Array.isArray(event.data[0])
                    ? event.data[0]
                    : event.data;
            if (Array.isArray(payload)) {
                queryClient.setQueryData<UpdateStatus[]>(["xxmi:updates"], payload);
            }
        });

        const removeXXMILaunchListener = Events.On("xxmi:launch-progress", (event) => {
            const payload = Array.isArray(event.data) ? event.data[0] : event.data;
            if (!payload || typeof payload !== "object") return;
            const { importer, stage, optimized } = payload as Record<string, unknown>;
            if (typeof importer !== "string" || typeof stage !== "string") return;
            const id = `xxmi-launch-${importer}`;
            if (stage === "ini-optimizer" && typeof optimized === "number") {
                toast.success(i18n.t("page.setting.xxmi.builtin.optimized", { count: optimized }));
            }
            if (stage === "finish" || stage === "failed") {
                toast.dismiss(id);
                void queryClient.invalidateQueries({ queryKey: ["xxmi:overview"] });
                return;
            }
            const status =
                stage === "ensure-runtime"
                    ? "launchDownloading"
                    : ["update-ini", "game-tweaks", "ini-optimizer", "pre-launch"].includes(stage)
                      ? "launchConfiguring"
                      : ["elevate", "inject-launch", "post-load"].includes(stage)
                        ? "launchStarting"
                        : "launchPreparing";
            toast.loading(`${importer} · ${i18n.t(`page.setting.xxmi.builtin.${status}`)}`, { id });
        });

        const removeXXMIPackageListener = Events.On("xxmi:package-progress", (event) => {
            const payload = Array.isArray(event.data) ? event.data[0] : event.data;
            if (!payload || typeof payload !== "object") return;
            const {
                package: pkg,
                version,
                stage,
                downloaded,
                total,
            } = payload as Record<string, unknown>;
            if (typeof pkg !== "string" || typeof stage !== "string") return;
            const id = `xxmi-package-${pkg}`;
            if (stage === "downloaded" || stage === "failed") {
                toast.dismiss(id);
                return;
            }
            if (stage !== "download" || typeof downloaded !== "number") return;
            const progress =
                typeof total === "number" && total > 0
                    ? `${Math.min(100, Math.round((downloaded / total) * 100))}%`
                    : `${Math.round(downloaded / (1024 * 1024))} MiB`;
            const label = typeof version === "string" && version ? `${pkg} ${version}` : pkg;
            toast.loading(
                `${label} · ${i18n.t("page.setting.xxmi.builtin.downloadingPackage")} ${progress}`,
                { id },
            );
        });

        return () => {
            removeToastListener();
            removeNaviListener();
            removePathSelectorListener();
            removeAuthListener();
            removeBackendStatusListener();
            removeLanguageListener();
            removeXXMIUpdatesListener();
            removeXXMILaunchListener();
            removeXXMIPackageListener();
        };
    }, [onPathSelectorModeSelect, i18n, queryClient]);
}

function whenSessionInitialized() {
    if (globalStore.getState().sessionInitialized) return Promise.resolve();

    return new Promise<void>((resolve) => {
        const unsubscribe = globalStore.subscribe((state) => {
            if (!state.sessionInitialized) return;
            unsubscribe();
            resolve();
        });

        if (globalStore.getState().sessionInitialized) {
            unsubscribe();
            resolve();
        }
    });
}
