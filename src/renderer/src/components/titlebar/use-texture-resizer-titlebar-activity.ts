import { buildTextureResizerTitlebarActivity } from "@renderer/components/titlebar/titlebar-activity";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import type { TextureResizeProgressEvent } from "@shared/types";
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

        void window.api
            .invoke("tools:getTextureResizeState")
            .then((state) => {
                if (disposed || hasLiveEvent) return;
                sync(state);
            })
            .catch((error) => {
                console.error("tools:getTextureResizeState failed for titlebar activity", error);
            });

        const removeListener = window.api.on("tools:textureResizeProgress", (event) => {
            hasLiveEvent = true;
            sync(event);
        });

        return () => {
            disposed = true;
            removeListener();
        };
    }, [t]);
}
