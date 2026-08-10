import { eden } from "@main/client";
import { chunk } from "es-toolkit";

export const DELETION_BATCH_SIZE = 500;

export type DeletionAccepted = {
    kind: "accepted";
    deletionJobId: string;
    status: "pending";
    deletionJobToken?: string;
};

export type DeletionCompleted = {
    kind: "completed";
    deletedCount: number;
};

export type DeletionResult = DeletionAccepted | DeletionCompleted;

export type BatchDeletionOutcome = {
    requestedIds: string[];
    acceptedIds: string[];
    jobs: DeletionAccepted[];
    errorMessage?: string;
};

export async function deleteDriveItems(uuids: string[]) {
    return runDeletionBatches(uuids, async (page) => {
        const { data, error } = await eden.akasha.content.delete_many.post({
            uuids: page,
        });
        return requireAccepted(resolveDeletionResult(data, error));
    });
}

export async function runDeletionBatches(
    ids: string[],
    request: (page: string[]) => Promise<DeletionAccepted | null>,
    batchSize = DELETION_BATCH_SIZE,
): Promise<BatchDeletionOutcome> {
    const requestedIds = [...new Set(ids)];
    if (requestedIds.length === 0) {
        return { requestedIds, acceptedIds: [], jobs: [] };
    }

    const acceptedIds: string[] = [];
    const jobs: DeletionAccepted[] = [];

    for (const page of chunk(requestedIds, batchSize)) {
        try {
            const job = await request(page);
            if (job) jobs.push(job);
            acceptedIds.push(...page);
        } catch (error) {
            return {
                requestedIds,
                acceptedIds,
                jobs,
                errorMessage: error instanceof Error ? error.message : String(error),
            };
        }
    }

    return { requestedIds, acceptedIds, jobs };
}

export function requireBatchAccepted(outcome: BatchDeletionOutcome) {
    if (outcome.acceptedIds.length === 0) {
        throw new Error(outcome.errorMessage || "delete_failed");
    }
    return outcome;
}

export function resolveDeletionResult(
    data: unknown,
    error: { status: number; value: unknown } | null,
): DeletionResult {
    if (error) {
        // Eden may surface explicit 202 responses on the error channel.
        if (error.status === 202) {
            const accepted = asDeletionAccepted(error.value);
            if (accepted) return accepted;
            const completed = asDeletionCompleted(error.value);
            if (completed) return completed;
        }
        throw new Error(toErrorMessage(error.value));
    }

    const accepted = asDeletionAccepted(data);
    if (accepted) return accepted;
    const completed = asDeletionCompleted(data);
    if (completed) return completed;

    throw new Error("unexpected_deletion_response");
}

/** Returns the accepted job, or null when the batch completed synchronously. */
function requireAccepted(result: DeletionResult): DeletionAccepted | null {
    if (result.kind === "accepted") return result;
    if (result.kind === "completed") return null;
    throw new Error("unexpected_deletion_response");
}

function asDeletionAccepted(value: unknown): DeletionAccepted | null {
    if (!value || typeof value !== "object") return null;
    if (!("deletionJobId" in value) || typeof value.deletionJobId !== "string") return null;
    if (!("status" in value) || value.status !== "pending") return null;
    return {
        kind: "accepted",
        deletionJobId: value.deletionJobId,
        status: "pending",
        deletionJobToken:
            "deletionJobToken" in value && typeof value.deletionJobToken === "string"
                ? value.deletionJobToken
                : undefined,
    };
}

function asDeletionCompleted(value: unknown): DeletionCompleted | null {
    if (!value || typeof value !== "object") return null;
    if (!("status" in value) || value.status !== "completed") return null;
    return {
        kind: "completed",
        deletedCount:
            "deletedCount" in value && typeof value.deletedCount === "number"
                ? value.deletedCount
                : 0,
    };
}

function toErrorMessage(value: unknown) {
    if (typeof value === "string") return value;
    if (value && typeof value === "object" && "message" in value) {
        return String(value.message);
    }
    return String(value);
}
