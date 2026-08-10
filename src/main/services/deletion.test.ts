import { beforeEach, describe, expect, it, vi } from "vitest";

const deleteManyPost = vi.fn();

vi.mock("@main/client", () => ({
    eden: {
        akasha: {
            content: {
                delete_many: {
                    post: (...args: unknown[]) => deleteManyPost(...args),
                },
            },
        },
    },
}));

import {
    DELETION_BATCH_SIZE,
    deleteDriveItems,
    requireBatchAccepted,
    resolveDeletionResult,
    runDeletionBatches,
} from "./deletion";

beforeEach(() => {
    deleteManyPost.mockReset();
});

describe("resolveDeletionResult", () => {
    it("accepts a 202-style payload from data", () => {
        expect(resolveDeletionResult({ deletionJobId: "job-1", status: "pending" }, null)).toEqual({
            kind: "accepted",
            deletionJobId: "job-1",
            status: "pending",
            deletionJobToken: undefined,
        });
    });

    it("accepts a 202 payload from the Eden error channel", () => {
        expect(
            resolveDeletionResult(null, {
                status: 202,
                value: {
                    deletionJobId: "job-2",
                    status: "pending",
                    deletionJobToken: "token",
                },
            }),
        ).toEqual({
            kind: "accepted",
            deletionJobId: "job-2",
            status: "pending",
            deletionJobToken: "token",
        });
    });

    it("accepts an empty-trash completed payload", () => {
        expect(resolveDeletionResult({ status: "completed", deletedCount: 0 }, null)).toEqual({
            kind: "completed",
            deletedCount: 0,
        });
    });

    it("throws non-202 Eden errors", () => {
        expect(() =>
            resolveDeletionResult(null, { status: 404, value: "items_not_found" }),
        ).toThrow("items_not_found");
    });
});

describe("runDeletionBatches", () => {
    it("chunks requests and returns accepted ids", async () => {
        const request = vi.fn(async (page: string[]) => ({
            kind: "accepted" as const,
            deletionJobId: `job-${page[0]}`,
            status: "pending" as const,
        }));

        const ids = Array.from({ length: DELETION_BATCH_SIZE + 1 }, (_, i) => `id-${i}`);
        const outcome = await runDeletionBatches(ids, request);

        expect(request).toHaveBeenCalledTimes(2);
        expect(request.mock.calls[0][0]).toHaveLength(DELETION_BATCH_SIZE);
        expect(request.mock.calls[1][0]).toHaveLength(1);
        expect(outcome.acceptedIds).toEqual(ids);
        expect(outcome.errorMessage).toBeUndefined();
    });

    it("keeps earlier accepted batches when a later batch fails", async () => {
        const request = vi.fn(async (page: string[]) => {
            if (page[0] === "b1") throw new Error("second_batch_failed");
            return {
                kind: "accepted" as const,
                deletionJobId: "job-a",
                status: "pending" as const,
            };
        });

        const outcome = await runDeletionBatches(["a1", "b1"], request, 1);

        expect(outcome.acceptedIds).toEqual(["a1"]);
        expect(outcome.jobs).toHaveLength(1);
        expect(outcome.errorMessage).toBe("second_batch_failed");
        expect(() => requireBatchAccepted(outcome)).not.toThrow();
    });

    it("throws through requireBatchAccepted when nothing was accepted", async () => {
        const outcome = await runDeletionBatches(["a"], async () => {
            throw new Error("boom");
        });

        expect(outcome.acceptedIds).toEqual([]);
        expect(() => requireBatchAccepted(outcome)).toThrow("boom");
    });

    it("treats a null job (synchronous completed batch) as accepted ids", async () => {
        const outcome = await runDeletionBatches(["a1", "a2"], async () => null);

        expect(outcome.acceptedIds).toEqual(["a1", "a2"]);
        expect(outcome.jobs).toEqual([]);
        expect(outcome.errorMessage).toBeUndefined();
    });
});

describe("deleteDriveItems", () => {
    it("succeeds when delete_many returns a synchronous completed payload", async () => {
        deleteManyPost.mockResolvedValue({
            data: { status: "completed", deletedCount: 2 },
            error: null,
        });

        const outcome = await deleteDriveItems(["id-1", "id-2"]);

        expect(deleteManyPost).toHaveBeenCalledWith({ uuids: ["id-1", "id-2"] });
        expect(outcome).toEqual({
            requestedIds: ["id-1", "id-2"],
            acceptedIds: ["id-1", "id-2"],
            jobs: [],
        });
        expect(() => requireBatchAccepted(outcome)).not.toThrow();
    });

    it("still rejects unexpected delete_many payloads", async () => {
        deleteManyPost.mockResolvedValue({
            data: { status: "mystery" },
            error: null,
        });

        const outcome = await deleteDriveItems(["id-1"]);

        expect(outcome.acceptedIds).toEqual([]);
        expect(outcome.errorMessage).toBe("unexpected_deletion_response");
    });
});
