import { Mod, type CompressionState } from "@bindings/mod";
import { Events } from "@wailsio/runtime";
import { useEffect, useState } from "react";

export function useModCompressionState() {
    const [state, setState] = useState<CompressionState | null>(null);

    useEffect(() => {
        let disposed = false;
        let hasLiveEvent = false;

        const off = Events.On("mod:compressionProgress", (event) => {
            hasLiveEvent = true;
            setState(event.data as CompressionState);
        });

        void Mod.GetCompressionState()
            .then((initialState) => {
                if (!disposed && !hasLiveEvent) setState(initialState);
            })
            .catch((error) => {
                console.error("mod:getCompressionState failed", error);
            });

        return () => {
            disposed = true;
            off();
        };
    }, []);

    return [state, setState] as const;
}
