import {
    buildXXMIUpdateFailureTitlebarActivity,
    buildXXMIUpdatesTitlebarActivity,
    type XXMIFailedStage,
} from "@renderer/components/titlebar/titlebar-activity";
import { installableUpdates, useXXMIUpdates } from "@renderer/hooks/use-xxmi-updates";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { Events } from "@wailsio/runtime";
import { omit } from "es-toolkit";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

const UPDATES_ACTIVITY_ID = "xxmi:updates";
const FAILURE_ACTIVITY_ID = "xxmi:update-failure";

export function useXXMITitlebarActivity() {
    const { t } = useTranslation();
    // CheckUpdates is empty under the external launcher or without importers, so this needs no overview,
    // which would read the process list on every window focus.
    const updates = useXXMIUpdates(true);
    const [failures, setFailures] = useState<Record<string, XXMIFailedStage[]>>({});
    const updateCount = installableUpdates(updates).length;
    const visibleFailures = useMemo(
        () =>
            Object.fromEntries(
                Object.entries(failures)
                    .map(([importer, stages]) => {
                        const visible = stages.filter(
                            (stage) =>
                                stage === "mod-runtimes" ||
                                // Installing or skipping the update by hand settles its failure. Without a
                                // result the check itself may be failing for the same reason, so it stays.
                                !updates ||
                                updates.some(
                                    (entry) =>
                                        entry.importer === importer &&
                                        entry.available &&
                                        !entry.pinned,
                                ),
                        );
                        return [importer, visible] as const;
                    })
                    .filter(([, stages]) => stages.length > 0),
            ),
        [failures, updates],
    );

    useEffect(
        () =>
            Events.On("xxmi:launch-progress", (event) => {
                const payload: unknown = Array.isArray(event.data) ? event.data[0] : event.data;
                if (!payload || typeof payload !== "object") return;
                const { importer, stage, detail } = payload as Record<string, unknown>;
                if (typeof importer !== "string") return;
                if (stage !== "auto-update" && stage !== "mod-runtimes") return;

                if (typeof detail === "string") {
                    setFailures((current) => ({
                        ...current,
                        [importer]: [...(current[importer] ?? []), stage],
                    }));
                    return;
                }

                // Every launch reports this stage first, which starts a new attempt for the importer.
                if (stage === "auto-update") {
                    setFailures((current) => omit(current, [importer]));
                }
            }),
        [],
    );

    useEffect(() => {
        const activity = buildXXMIUpdatesTitlebarActivity(updateCount, t);
        if (activity) {
            titlebarActivityStore.getState().upsertActivity(activity);
            return;
        }
        titlebarActivityStore.getState().removeActivity(UPDATES_ACTIVITY_ID);
    }, [updateCount, t]);

    useEffect(() => {
        const activity = buildXXMIUpdateFailureTitlebarActivity(visibleFailures, t);
        if (activity) {
            titlebarActivityStore.getState().upsertActivity(activity);
            return;
        }
        titlebarActivityStore.getState().removeActivity(FAILURE_ACTIVITY_ID);
    }, [visibleFailures, t]);

    useEffect(
        () => () => {
            titlebarActivityStore.getState().removeActivity(UPDATES_ACTIVITY_ID);
            titlebarActivityStore.getState().removeActivity(FAILURE_ACTIVITY_ID);
        },
        [],
    );
}
