export interface ISemaphore {
    acquire(): Promise<void>;
    release(): void;
    count(): number;
    setMax(newMax: number): void;
    purge(): number;
}

/**
 * Basic Semaphore implementation.
 * @date 2/15/2024 - 4:52:51 AM
 *
 * @type {new (max: number) => ISemaphore}
 */
export const Semaphore = function (this: ISemaphore, max: number) {
    let counter = 0;
    let waiting: {
        resolve: (value: void | PromiseLike<void>) => void;
        err: (reason?: unknown) => void;
    }[] = [];
    let maxCount = max || 1;

    const take = (): void => {
        if (waiting.length > 0 && counter < maxCount) {
            counter++;

            const promise = waiting.shift();

            if (!promise) {
                return;
            }

            promise.resolve();
        }
    };

    this.acquire = (): Promise<void> => {
        if (counter < maxCount) {
            counter++;

            return new Promise<void>((resolve) => {
                resolve();
            });
        } else {
            return new Promise<void>((resolve, err) => {
                waiting.push({
                    resolve: resolve,
                    err: err,
                });
            });
        }
    };

    this.release = (): void => {
        counter--;

        take();
    };

    this.count = (): number => counter;

    this.setMax = (newMax: number): void => {
        maxCount = newMax;
    };

    this.purge = (): number => {
        const unresolved = waiting.length;

        for (let i = 0; i < unresolved; i++) {
            const w = waiting[i];

            if (!w) {
                continue;
            }

            w.err("Task has been purged");
        }

        counter = 0;
        waiting = [];

        return unresolved;
    };
} as unknown as { new (max: number): ISemaphore };
