import { type TitlebarActivity, titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { useEffect, useRef } from "react";

/**
 * Mirrors `activity` into the titlebar store.
 * Passing `null` removes the previously registered id.
 * Unmount does not remove — badges stay until explicit null/remove or a replacing upsert.
 */
export function useTitlebarActivity(activity: TitlebarActivity | null) {
    const registeredIdRef = useRef<string | null>(null);

    useEffect(() => {
        if (!activity) {
            if (registeredIdRef.current) {
                titlebarActivityStore.getState().removeActivity(registeredIdRef.current);
                registeredIdRef.current = null;
            }
            return;
        }

        if (registeredIdRef.current && registeredIdRef.current !== activity.id) {
            titlebarActivityStore.getState().removeActivity(registeredIdRef.current);
        }

        registeredIdRef.current = activity.id;
        titlebarActivityStore.getState().upsertActivity(activity);
    }, [activity]);
}
