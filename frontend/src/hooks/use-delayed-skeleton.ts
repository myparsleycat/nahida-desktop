import { useEffect, useState } from "react";

export function useDelayedSkeleton(isLoading: boolean, delay = 1000) {
    const [delayed, setDelayed] = useState(false);
    const [prevLoading, setPrevLoading] = useState(isLoading);

    if (isLoading !== prevLoading) {
        setPrevLoading(isLoading);
        setDelayed(false);
    }

    useEffect(() => {
        if (!isLoading) {
            return;
        }

        const timer = setTimeout(() => {
            setDelayed(true);
        }, delay);

        return () => {
            clearTimeout(timer);
        };
    }, [isLoading, delay]);

    return isLoading && delayed;
}
