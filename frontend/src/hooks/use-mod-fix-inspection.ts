import { Tools, type FixInspectionRecord, type FixInspectionSnapshot } from "@bindings/tools";
import { buildModFixTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { Logger } from "@renderer/lib/logger";
import { modStore } from "@renderer/store/mod";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { useNavigate } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

const fixInspectionEvent = "tools:fix-inspections";
const fixActivityPrefix = "mod-fix:";

export function useModFixInspectionTitlebarActivity() {
    const navigate = useNavigate();
    const { t } = useTranslation();
    const latestRevision = useRef(-1);

    const applySnapshot = useCallback(
        (snapshot: FixInspectionSnapshot) => {
            if (snapshot.revision < latestRevision.current) {
                return;
            }
            latestRevision.current = snapshot.revision;
            syncFixInspectionActivities(
                snapshot,
                (record) => {
                    modStore.getState().setPendingModFixerRequest({
                        modPath: record.modPath,
                        importer: record.result.importer,
                        actionTool: record.result.actionTool || undefined,
                    });
                    void navigate({ to: "/mod" });
                },
                t,
            );
        },
        [navigate, t],
    );

    useEffect(() => {
        const off = Events.On(fixInspectionEvent, (event) => {
            applySnapshot(event.data as FixInspectionSnapshot);
        });
        const refresh = () => {
            void Tools.RefreshFixInspections()
                .then(applySnapshot)
                .catch((error: unknown) => {
                    Logger.error(error, "ModFixInspection:restore");
                });
        };
        const offFocus = Events.On("window:focus", refresh);

        refresh();

        return () => {
            off();
            offFocus();
        };
    }, [applySnapshot]);
}

export function syncFixInspectionActivities(
    snapshot: FixInspectionSnapshot,
    onOpenFixer: (record: FixInspectionRecord) => void,
    t: (key: string, opts?: Record<string, unknown>) => string,
) {
    const records = snapshot.inspections ?? [];
    const activeIds = new Set(records.map((record) => `${fixActivityPrefix}${record.modPath}`));
    const store = titlebarActivityStore.getState();

    for (const id of Object.keys(store.activities)) {
        if (id.startsWith(fixActivityPrefix) && !activeIds.has(id)) {
            store.removeActivity(id);
        }
    }

    for (const record of records) {
        store.upsertActivity(
            buildModFixTitlebarActivity({
                modPath: record.modPath,
                displayName: record.displayName,
                result: record.result,
                onOpenFixer: () => onOpenFixer(record),
                t,
            }),
        );
    }
}
