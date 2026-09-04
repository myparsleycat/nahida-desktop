import { buildModCompressionTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { useModCompressionState } from "@renderer/hooks/use-mod-compression-state";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const ACTIVITY_ID = "mod:compression";

export function useModCompressionTitlebarActivity() {
    const { t } = useTranslation();
    const [state] = useModCompressionState();

    useEffect(() => {
        const activity = buildModCompressionTitlebarActivity(state, t);
        if (activity) {
            titlebarActivityStore.getState().upsertActivity(activity);
            return;
        }
        titlebarActivityStore.getState().removeActivity(ACTIVITY_ID);
    }, [state, t]);

    useEffect(() => () => titlebarActivityStore.getState().removeActivity(ACTIVITY_ID), []);
}
