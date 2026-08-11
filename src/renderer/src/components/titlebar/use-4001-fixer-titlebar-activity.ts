import { build4001FixerTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const FIXER_ACTIVITY_ID = "tools:4001-fixer";

export function use4001FixerTitlebarActivity() {
    const { t } = useTranslation();

    useEffect(() => {
        const sync = (task: Parameters<typeof build4001FixerTitlebarActivity>[0], code = "") => {
            const activity = build4001FixerTitlebarActivity(task, code, t);
            if (activity) {
                titlebarActivityStore.getState().upsertActivity(activity);
                return;
            }
            titlebarActivityStore.getState().removeActivity(FIXER_ACTIVITY_ID);
        };

        void window.api.invoke("tools:4001FixerGetState").then((state) => {
            sync(state.activeTask, state.progress || "");
        });

        return window.api.on("tools:4001FixerProgress", (event) => {
            sync(event.task, event.code);
        });
    }, [t]);
}
