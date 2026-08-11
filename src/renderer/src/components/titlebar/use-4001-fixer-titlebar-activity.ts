import { build4001FixerTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const FIXER_ACTIVITY_ID = "tools:4001-fixer";

export function use4001FixerTitlebarActivity() {
    const { t } = useTranslation();

    useEffect(() => {
        let disposed = false;
        let hasLiveEvent = false;

        const sync = (task: Parameters<typeof build4001FixerTitlebarActivity>[0], code = "") => {
            const activity = build4001FixerTitlebarActivity(task, code, t);
            if (activity) {
                titlebarActivityStore.getState().upsertActivity(activity);
                return;
            }
            titlebarActivityStore.getState().removeActivity(FIXER_ACTIVITY_ID);
        };

        void window.api
            .invoke("tools:4001FixerGetState")
            .then((state) => {
                if (disposed || hasLiveEvent) return;
                sync(state.activeTask, state.progress || "");
            })
            .catch((error) => {
                console.error("tools:4001FixerGetState failed for titlebar activity", error);
            });

        const removeListener = window.api.on("tools:4001FixerProgress", (event) => {
            hasLiveEvent = true;
            sync(event.task, event.code);
        });

        return () => {
            disposed = true;
            removeListener();
        };
    }, [t]);
}
