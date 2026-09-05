import { Tools } from "@bindings/tools";
import { buildTextureResizerTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { Logger } from "@renderer/lib/logger";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import type { TextureResizeProgressEvent } from "@shared/types";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";

const TEXTURE_RESIZER_ACTIVITY_ID = "tools:texture-resizer";

export function useTextureResizerTitlebarActivity() {
    const { t } = useTranslation();

    useEffect(() => {
        let disposed = false;
        let hasLiveEvent = false;

        const sync = (event: TextureResizeProgressEvent | null) => {
            const activity = buildTextureResizerTitlebarActivity(event, t);
            if (activity) {
                titlebarActivityStore.getState().upsertActivity(activity);
                return;
            }
            titlebarActivityStore.getState().removeActivity(TEXTURE_RESIZER_ACTIVITY_ID);
        };

        void Tools.GetTextureResizeState()
            .then((state) => {
                if (disposed || hasLiveEvent) return;
                sync(state as Parameters<typeof sync>[0]);
            })
            .catch((error) => {
                Logger.capture(
                    "components/titlebar/use-texture-resizer-titlebar-activity.ts",
                    "tools:getTextureResizeState failed for titlebar activity",
                    error,
                );
            });

        const off = Events.On("tools:textureResizeProgress", (event) => {
            hasLiveEvent = true;
            sync(event.data as Parameters<typeof sync>[0]);
        });

        return () => {
            disposed = true;
            off();
        };
    }, [t]);
}
