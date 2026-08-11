import { buildModBisectTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import type { BisectSnapshot } from "@shared/types";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const BISECT_ACTIVITY_ID = "tools:mod-bisect";

export function useModBisectTitlebarActivity() {
    const { t } = useTranslation();

    useEffect(() => {
        const sync = (snapshot: BisectSnapshot | null) => {
            const activity = buildModBisectTitlebarActivity(snapshot, t);
            if (activity) {
                titlebarActivityStore.getState().upsertActivity(activity);
                return;
            }
            titlebarActivityStore.getState().removeActivity(BISECT_ACTIVITY_ID);
        };

        void window.api.invoke("tools:bisectGetState").then(sync);

        return window.api.on("tools:bisectState", sync);
    }, [t]);
}
