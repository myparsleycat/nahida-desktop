import { Tools } from "@bindings/tools";
import { buildModBisectTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import type { BisectSnapshot } from "@shared/types";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const BISECT_ACTIVITY_ID = "tools:mod-bisect";

export function useModBisectTitlebarActivity() {
    const { t } = useTranslation();

    useEffect(() => {
        let disposed = false;
        let hasLiveEvent = false;

        const sync = (snapshot: BisectSnapshot | null) => {
            const activity = buildModBisectTitlebarActivity(snapshot, t);
            if (activity) {
                titlebarActivityStore.getState().upsertActivity(activity);
                return;
            }
            titlebarActivityStore.getState().removeActivity(BISECT_ACTIVITY_ID);
        };

        void Tools.BisectGetState()
            .then((snapshot) => {
                if (disposed || hasLiveEvent) return;
                sync(snapshot as Parameters<typeof sync>[0]);
            })
            .catch((error) => {
                console.error("tools:bisectGetState failed for titlebar activity", error);
            });

        const off = Events.On("tools:bisectState", (event) => {
            hasLiveEvent = true;
            sync(event.data as Parameters<typeof sync>[0]);
        });

        return () => {
            disposed = true;
            off();
        };
    }, [t]);
}
