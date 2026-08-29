import { isCurrentRequest, resolveIfCurrent } from "@renderer/lib/generation-gate";
import { describe, expect, it } from "vitest";

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((res) => {
        resolve = res;
    });
    return { promise, resolve };
}

type Inspection = { modRoot: string };

async function loadTouchProfileInspection(
    latestId: { current: number },
    targetPath: string,
    prepare: (modPath: string) => Promise<Inspection>,
    commit: {
        setLoading: (loading: boolean) => void;
        setInspection: (inspection: Inspection) => void;
    },
) {
    const requestId = ++latestId.current;
    if (isCurrentRequest(requestId, latestId)) {
        commit.setLoading(true);
    }
    try {
        const next = await resolveIfCurrent(requestId, latestId, () => prepare(targetPath));
        if (next === undefined) {
            return;
        }
        commit.setInspection(next);
    } finally {
        if (isCurrentRequest(requestId, latestId)) {
            commit.setLoading(false);
        }
    }
}

describe("touch-profile loadMod generation", () => {
    it("keeps B's inspection when fixedTargetPath changes from A to B while A is pending", async () => {
        const latestId = { current: 0 };
        let loading = false;
        let inspection: Inspection | null = null;
        const pendingA = deferred<Inspection>();
        const pendingB = deferred<Inspection>();
        const inspectionA = { modRoot: "A" };
        const inspectionB = { modRoot: "B" };

        const loadA = loadTouchProfileInspection(latestId, "A", () => pendingA.promise, {
            setLoading: (next) => {
                loading = next;
            },
            setInspection: (next) => {
                inspection = next;
            },
        });
        const loadB = loadTouchProfileInspection(latestId, "B", () => pendingB.promise, {
            setLoading: (next) => {
                loading = next;
            },
            setInspection: (next) => {
                inspection = next;
            },
        });

        pendingA.resolve(inspectionA);
        await loadA;
        expect(inspection).toBeNull();
        expect(loading).toBe(true);

        pendingB.resolve(inspectionB);
        await loadB;
        expect(inspection).toEqual(inspectionB);
        expect(loading).toBe(false);
    });
});
