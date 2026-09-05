import { Tools } from "@bindings/tools";
import { build4001FixerTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { Logger } from "@renderer/lib/logger";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { Events } from "@wailsio/runtime";
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

        void Tools.FourThousandOneFixerGetState()
            .then((state) => {
                if (disposed || hasLiveEvent) return;
                sync(state.activeTask as Parameters<typeof sync>[0], state.progress || "");
            })
            .catch((error) => {
                Logger.capture(
                    "components/titlebar/use-4001-fixer-titlebar-activity.ts",
                    "tools:4001FixerGetState failed for titlebar activity",
                    error,
                );
            });

        const off = Events.On("tools:4001FixerProgress", (event) => {
            hasLiveEvent = true;
            const payload = event.data as { task?: string; code?: string };
            sync(payload.task as Parameters<typeof sync>[0], payload.code);
        });

        return () => {
            disposed = true;
            off();
        };
    }, [t]);
}
