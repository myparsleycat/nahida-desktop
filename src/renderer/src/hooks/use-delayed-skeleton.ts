import { useEffect, useState } from "react";

export function useDelayedSkeleton(isLoading: boolean, delay = 1000) {
    const [shouldShow, setShouldShow] = useState(false);

    useEffect(() => {
        let timer: NodeJS.Timeout;

        if (isLoading) {
            timer = setTimeout(() => {
                setShouldShow(true);
            }, delay);
        } else {
            setShouldShow(false);
        }

        return () => {
            if (timer) clearTimeout(timer);
        };
    }, [isLoading, delay]);

    return shouldShow;
}
