import { randomUUID } from "node:crypto";
import { open, rm, type FileHandle } from "node:fs/promises";

import { networkFetch } from "@main/internal/network-fetch";
import { parseHttpBody, readApiBody } from "@main/lib/cbor-response";
import { BACKEND_URL } from "@shared/const";
import type { PlanPhase } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import ky from "ky";
import pLimit from "p-limit";
import type PQueue from "p-queue";
import { parseServerSentEvents } from "parse-sse";

import type { NahidaDesktop } from "..";
import type { FinalFile, UploadProgress } from "./upload";
import type { PreparedDirectFile } from "./upload-compress";

import {
    creditedLogicalBytesForMember,
    DIRECT_UPLOAD_THRESHOLD,
    logicalBytesForPackProgress,
    PACK_MAX_FILES,
    PACK_PAYLOAD_BUDGET,
    packUploadUrl,
    partitionPackedUploads,
} from "./upload-pack";

const PLAN_PAGE_SIZE = 500;
const PART_SIZE = 25 * 1024 * 1024;
export const MAX_UPLOAD_FILE_SIZE = 1024 ** 3;
export const MAX_MULTIPART_CONCURRENCY = 4;
const RETRY_LIMIT = 3;
const COMPLETE_TIMEOUT_MS = 15 * 60 * 1000;
const UPLOAD_STREAM_CHUNK_SIZE = 64 * 1024;

type UploadPlanItem = {
    clientId: string;
    status: "created" | "pending" | "exists" | "denied" | "error";
    reason?: string;
    itemId?: string;
    intentId?: string;
    bundleId?: string;
};

type UploadPlanEntry = {
    intentId: string;
    url: string;
    method: "POST";
    form: { token: string; sha256: string };
};

type IntentPackResult = {
    intentId: string;
    status: "completed" | "failed" | "pending";
    fileId?: string;
    reason?: string;
};

type NteBundle = {
    id: string;
    memberClientIds: string[];
    completeUrl: string;
    abortUrl: string;
    form: { token: string };
};

type HttpResult = {
    status: number;
    reason?: string;
    payload?: { status?: string; results?: IntentPackResult[]; code?: string };
};

type PreparedUpload = {
    upload: UploadPlanEntry;
    source: FinalFile;
    copies: FinalFile[];
    prepared: { data: Buffer; compAlg?: "zstd" };
    payloadBytes: number;
    logicalSize: number;
};

export class UploadV2Error extends Error {
    public readonly code: string;

    public constructor(code: string, message = code) {
        super(message);
        this.name = "UploadV2Error";
        this.code = code;
    }
}

export function uploadErrorCode(error: unknown) {
    if (typeof error !== "object" || error === null) return undefined;
    const code = (error as { code?: unknown }).code;
    return typeof code === "string" ? code : undefined;
}

export function paginateUploadFiles(files: FinalFile[], pageSize = PLAN_PAGE_SIZE) {
    const grouped = new Map<string, FinalFile[]>();
    const units: FinalFile[][] = [];

    for (const file of files) {
        const key = nteGroupKey(file);
        if (!key) {
            units.push([file]);
            continue;
        }
        const group = grouped.get(key);
        if (group) {
            group.push(file);
            continue;
        }
        const created = [file];
        grouped.set(key, created);
        units.push(created);
    }

    const pages: FinalFile[][] = [];
    let page: FinalFile[] = [];
    for (const unit of units) {
        if (unit.length > pageSize) throw new UploadV2Error("nte_bundle_too_large");
        if (page.length > 0 && page.length + unit.length > pageSize) {
            pages.push(page);
            page = [];
        }
        page.push(...unit);
        if (page.length >= pageSize) {
            pages.push(page);
            page = [];
        }
    }
    if (page.length > 0) pages.push(page);
    return pages;
}

function nteGroupKey(file: FinalFile) {
    const match = /^(.*)\.(pak|utoc|ucas)$/i.exec(file.name);
    if (!match) return undefined;
    const basename =
        match[2].toLowerCase() === "ucas" ? match[1].replace(/_s[1-9]\d*$/i, "") : match[1];
    return `${file.parentId}\0${basename.normalize("NFC").toLowerCase()}`;
}

export async function uploadDriveFilesV2({
    desktop,
    currentId,
    requestId,
    files,
    queue,
    prepareDirectFile,
    onProgress,
    onPlanProgress,
    onPlanComplete,
    signal,
}: {
    desktop: NahidaDesktop;
    currentId: string;
    requestId: string;
    files: FinalFile[];
    queue: PQueue;
    prepareDirectFile: (file: FinalFile) => Promise<PreparedDirectFile>;
    onProgress?: (progress: UploadProgress) => void;
    onPlanProgress?: (progress: { phase: PlanPhase; processed: number; total: number }) => void;
    onPlanComplete?: () => void;
    signal?: AbortSignal;
}) {
    const oversized = files.find((file) => file.size > MAX_UPLOAD_FILE_SIZE);
    if (oversized) {
        throw new UploadV2Error(
            "upload_file_too_large",
            `${oversized.name}: upload_file_too_large`,
        );
    }

    const items: UploadPlanItem[] = [];
    const uploads = new Map<string, UploadPlanEntry>();
    const nteBundles = new Map<string, NteBundle>();

    const url = `${BACKEND_URL}/akasha/v2/sse/drive/files:plan`;
    const pages = paginateUploadFiles(files);
    let plannedFiles = 0;
    for (const page of pages) {
        signal?.throwIfAborted();
        const response = await networkFetch(url, {
            method: "POST",
            headers: {
                ...(await desktop.httpService.getHeaders(url)),
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                requestId,
                current: currentId,
                capabilities: ["nte-bundle-v1"],
                files: page.map((file) => ({
                    clientId: file.FID,
                    name: file.name,
                    sha256: file.sha256,
                    size: file.size,
                    parentId: file.parentId,
                    path: file.path,
                })),
            }),
            signal,
        });
        if (!response.ok) {
            const body = await readApiBody(response).catch(() => response.statusText);
            throw apiError(body, `[upload plan failed] ${toErrorMessage(body)}`);
        }
        if (!response.body) {
            throw new Error("[upload plan failed] empty response stream");
        }

        for await (const event of parseServerSentEvents(response)) {
            if (event.type === "error") {
                throw apiError(parseEventData(event.data), `[upload plan failed] ${event.data}`);
            }
            if (event.type === "progress") {
                const data = JSON.parse(event.data) as {
                    phase: PlanPhase;
                    processed: number;
                    total: number;
                };
                onPlanProgress?.({
                    phase: data.phase,
                    processed: plannedFiles + data.processed,
                    total: files.length,
                });
            }
            if (event.type === "complete") {
                const data = JSON.parse(event.data) as {
                    items: UploadPlanItem[];
                    uploads: UploadPlanEntry[];
                    nteBundles?: NteBundle[];
                };
                items.push(...data.items);
                data.uploads.forEach((upload) => uploads.set(upload.intentId, upload));
                data.nteBundles?.forEach((bundle) => nteBundles.set(bundle.id, bundle));
            }
        }
        plannedFiles += page.length;
    }

    const filesById = new Map(files.map((file) => [file.FID, file]));
    const bundleIdByClientId = new Map(
        [...nteBundles.values()].flatMap((bundle) =>
            bundle.memberClientIds.map((clientId) => [clientId, bundle.id] as const),
        ),
    );
    items.forEach((item) => {
        if (item.bundleId) bundleIdByClientId.set(item.clientId, item.bundleId);
    });
    const pendingByIntent = new Map<string, FinalFile[]>();
    const rejected: UploadPlanItem[] = [];
    const returnedClientIds = new Set(items.map((item) => item.clientId));
    const stagedClientIds = new Set<string>();
    const bundleCreditedBytes = new Map<string, number>();
    const reportFileBytes = (file: FinalFile, bytes: number, isServerDeduplicated: boolean) => {
        const bundleId = bundleIdByClientId.get(file.FID);
        if (bundleId) {
            bundleCreditedBytes.set(bundleId, (bundleCreditedBytes.get(bundleId) ?? 0) + bytes);
        }
        onProgress?.({ bytes, isServerDeduplicated });
    };
    const markReady = (file: FinalFile, bytes: number, isServerDeduplicated: boolean) => {
        if (bundleIdByClientId.has(file.FID)) {
            stagedClientIds.add(file.FID);
            reportFileBytes(file, bytes, isServerDeduplicated);
            return;
        }
        onProgress?.({ bytes, fileId: file.FID, isServerDeduplicated });
    };

    for (const item of items) {
        const file = filesById.get(item.clientId);
        if (!file) continue;
        if (item.status === "created" || item.status === "exists") {
            markReady(file, file.size, true);
            continue;
        }
        if (item.status === "pending" && item.intentId) {
            pendingByIntent.set(item.intentId, [
                ...(pendingByIntent.get(item.intentId) ?? []),
                file,
            ]);
            continue;
        }
        rejected.push(item);
    }
    files
        .filter((file) => !returnedClientIds.has(file.FID))
        .forEach((file) =>
            rejected.push({
                clientId: file.FID,
                status: "error",
                reason: "upload_plan_item_missing",
            }),
        );

    // Clear planning UI only after plan-side progress (e.g. server dedup) is applied.
    onPlanComplete?.();

    const failures: Error[] = [];
    const failedBundles = new Set<string>();
    const bundleControllers = new Map(
        [...nteBundles.keys()].map((bundleId) => [bundleId, new AbortController()]),
    );
    const abortRequests = new Map<string, Promise<void>>();
    const abortBundle = (bundleId: string, reason: unknown) => {
        const existing = abortRequests.get(bundleId);
        if (existing) return existing;
        bundleControllers.get(bundleId)?.abort(reason);
        const bundle = nteBundles.get(bundleId);
        if (!bundle) return Promise.resolve();
        const request = sendJson(
            desktop,
            bundle.abortUrl,
            { token: bundle.form.token },
            AbortSignal.timeout(30_000),
        ).then((result) => {
            if (result.status < 200 || result.status >= 300) throw httpError(result);
        });
        abortRequests.set(bundleId, request);
        return request;
    };
    const failTargets = (error: unknown, targets: FinalFile[], context: string) => {
        const normalized = error instanceof Error ? error : new Error(String(error));
        const bundleIds = new Set(
            targets
                .map((file) => bundleIdByClientId.get(file.FID))
                .filter((bundleId): bundleId is string => bundleId !== undefined),
        );
        if (bundleIds.size === 0 || targets.some((file) => !bundleIdByClientId.has(file.FID))) {
            failures.push(normalized);
        }
        for (const bundleId of bundleIds) {
            if (failedBundles.has(bundleId)) continue;
            failedBundles.add(bundleId);
            failures.push(normalized);
            void abortBundle(bundleId, normalized).catch((cleanupError) => {
                desktop.logger.error(cleanupError, `UploadV2:bundle-abort:${bundleId}:${context}`);
            });
        }
    };
    const intentSignal = (targets: FinalFile[]) => {
        const bundleIds = [
            ...new Set(
                targets
                    .map((file) => bundleIdByClientId.get(file.FID))
                    .filter((bundleId): bundleId is string => bundleId !== undefined),
            ),
        ];
        if (bundleIds.length !== 1) return signal;
        const bundleSignal = bundleControllers.get(bundleIds[0])?.signal;
        if (!bundleSignal) return signal;
        return signal ? AbortSignal.any([signal, bundleSignal]) : bundleSignal;
    };
    const markIntentReady = (source: FinalFile, copies: FinalFile[]) => {
        markReady(source, 0, false);
        copies.forEach((file) => markReady(file, file.size, true));
    };
    const multipartLimit = pLimit(MAX_MULTIPART_CONCURRENCY);
    const rolledBackBundles = new Set<string>();
    const rollbackFailedBundles = () => {
        for (const bundleId of failedBundles) {
            if (rolledBackBundles.has(bundleId)) continue;
            rolledBackBundles.add(bundleId);
            const credited = bundleCreditedBytes.get(bundleId) ?? 0;
            if (credited > 0) {
                onProgress?.({ bytes: -credited, isServerDeduplicated: false });
            }
        }
    };
    const packed: PreparedUpload[] = [];
    const flushPacked = () => {
        const groups = partitionPackedUploads(packed.splice(0));
        for (const group of groups) {
            void queue.add(async () => {
                try {
                    if (group.kind === "single") {
                        await uploadPreparedIntent({
                            desktop,
                            member: group.member,
                            signal,
                            onProgress,
                            onIntentReady: markIntentReady,
                        });
                        return;
                    }
                    await uploadPack({
                        desktop,
                        members: group.members,
                        signal,
                        onProgress,
                        onIntentReady: markIntentReady,
                    });
                } catch (error) {
                    if (!signal?.aborted) {
                        desktop.logger.error(error, "UploadV2:pack");
                        failures.push(error instanceof Error ? error : new Error(String(error)));
                    }
                }
            });
        }
    };

    const abortAllBundles = () => {
        nteBundles.forEach((bundle) => {
            void abortBundle(bundle.id, signal?.reason).catch((cleanupError) => {
                desktop.logger.error(cleanupError, `UploadV2:bundle-abort:${bundle.id}:cancel`);
            });
        });
    };
    for (const item of rejected) {
        const file = filesById.get(item.clientId);
        if (!file || !bundleIdByClientId.has(file.FID)) continue;
        failTargets(
            new UploadV2Error(
                item.reason ?? item.status,
                `${file.name}: ${item.reason ?? item.status}`,
            ),
            [file],
            "plan",
        );
    }
    signal?.addEventListener("abort", abortAllBundles, { once: true });

    try {
        for (const [intentId, targets] of pendingByIntent.entries()) {
            signal?.throwIfAborted();
            const targetBundleIds = targets
                .map((file) => bundleIdByClientId.get(file.FID))
                .filter((bundleId): bundleId is string => bundleId !== undefined);
            if (
                targetBundleIds.length > 0 &&
                targets.every((file) => bundleIdByClientId.has(file.FID)) &&
                targetBundleIds.every((bundleId) => failedBundles.has(bundleId))
            ) {
                continue;
            }
            const upload = uploads.get(intentId);
            if (!upload) {
                if (targets.every((file) => bundleIdByClientId.has(file.FID))) {
                    targets.forEach((file) => markReady(file, file.size, true));
                    continue;
                }
                failures.push(new Error(`Upload intent missing for ${targets[0].name}`));
                continue;
            }
            const source = targets[0];
            const targetsSignal = intentSignal(targets);
            if (source.size >= DIRECT_UPLOAD_THRESHOLD) {
                void queue.add(async () => {
                    try {
                        await multipartLimit(async () => {
                            let prepared: PreparedDirectFile | undefined;
                            try {
                                prepared = await prepareDirectFile(source);
                                await uploadParts(
                                    desktop,
                                    upload,
                                    source,
                                    prepared,
                                    targetsSignal,
                                    (bytes) => reportFileBytes(source, bytes, false),
                                );
                                markIntentReady(source, targets.slice(1));
                            } finally {
                                if (prepared?.cleanupPath && prepared.path) {
                                    await rm(prepared.path, { force: true });
                                }
                            }
                        });
                    } catch (error) {
                        if (signal?.aborted) return;
                        const targetBundleIds = targets
                            .map((file) => bundleIdByClientId.get(file.FID))
                            .filter((bundleId): bundleId is string => bundleId !== undefined);
                        if (
                            targetsSignal?.aborted &&
                            targetBundleIds.every((id) => failedBundles.has(id))
                        ) {
                            return;
                        }
                        desktop.logger.error(error, `UploadV2:intent:${intentId}`);
                        failTargets(error, targets, `intent:${intentId}`);
                    }
                });
                continue;
            }
            let prepared: PreparedDirectFile;
            try {
                prepared = await prepareDirectFile(source);
            } catch (error) {
                failTargets(error, targets, `prepare:${intentId}`);
                continue;
            }
            if (!prepared.data) {
                failTargets(
                    new Error("prepared_payload_missing"),
                    targets,
                    `prepare:${intentId}`,
                );
                continue;
            }
            const member = {
                upload,
                source,
                copies: targets.slice(1),
                prepared: { data: prepared.data, compAlg: prepared.compAlg },
                payloadBytes: prepared.data.byteLength,
                logicalSize: source.size,
            };
            if (targets.some((file) => bundleIdByClientId.has(file.FID))) {
                void queue.add(async () => {
                    try {
                        await uploadPreparedIntent({
                            desktop,
                            member,
                            signal: targetsSignal,
                            onProgress: (progress) =>
                                reportFileBytes(
                                    source,
                                    progress.bytes,
                                    progress.isServerDeduplicated ?? false,
                                ),
                            onIntentReady: markIntentReady,
                        });
                    } catch (error) {
                        if (signal?.aborted) return;
                        const targetBundleIds = targets
                            .map((file) => bundleIdByClientId.get(file.FID))
                            .filter((bundleId): bundleId is string => bundleId !== undefined);
                        if (
                            targetsSignal?.aborted &&
                            targetBundleIds.every((id) => failedBundles.has(id))
                        ) {
                            return;
                        }
                        desktop.logger.error(error, `UploadV2:intent:${intentId}`);
                        failTargets(error, targets, `intent:${intentId}`);
                    }
                });
                continue;
            }
            packed.push(member);
            const packedBytes = packed.reduce((sum, entry) => sum + entry.payloadBytes, 0);
            if (packed.length >= PACK_MAX_FILES || packedBytes >= PACK_PAYLOAD_BUDGET) {
                flushPacked();
                await queue.onSizeLessThan(Math.max(1, queue.concurrency));
                signal?.throwIfAborted();
            }
        }
        flushPacked();
        await queue.onIdle();
        signal?.throwIfAborted();
        rollbackFailedBundles();

        for (const bundle of nteBundles.values()) {
            if (failedBundles.has(bundle.id)) continue;
            const members = bundle.memberClientIds
                .map((clientId) => filesById.get(clientId))
                .filter((file): file is FinalFile => file !== undefined);
            if (
                members.length !== bundle.memberClientIds.length ||
                members.some((file) => !stagedClientIds.has(file.FID))
            ) {
                const error = new UploadV2Error(
                    "nte_bundle_incomplete",
                    `${members[0]?.name ?? bundle.id}: nte_bundle_incomplete`,
                );
                failTargets(error, members, `complete:${bundle.id}`);
                continue;
            }
            try {
                await completeNteBundle(desktop, bundle, signal);
                members.forEach((file) =>
                    onProgress?.({ bytes: 0, fileId: file.FID, isServerDeduplicated: false }),
                );
            } catch (error) {
                failTargets(error, members, `complete:${bundle.id}`);
            }
        }
        rollbackFailedBundles();
    } finally {
        signal?.removeEventListener("abort", abortAllBundles);
        if (signal?.aborted) {
            abortAllBundles();
            await Promise.allSettled(abortRequests.values());
        }
    }

    if (rejected.length > 0) {
        const message = rejected
            .map(
                (item) =>
                    `${filesById.get(item.clientId)?.name ?? item.clientId}: ${item.reason ?? item.status}`,
            )
            .join(", ");
        const code = rejected.find((item) => item.reason)?.reason;
        failures.push(code ? new UploadV2Error(code, message) : new Error(message));
    }
    await Promise.allSettled(abortRequests.values());
    if (failures.length > 0) {
        const coded = failures.find((error) => uploadErrorCode(error));
        if (coded) throw coded;
        throw new Error(failures.map((error) => error.message).join("\n"));
    }
}

async function completeNteBundle(desktop: NahidaDesktop, bundle: NteBundle, signal?: AbortSignal) {
    const startedAt = Date.now();
    for (let attempt = 0; Date.now() - startedAt < COMPLETE_TIMEOUT_MS; attempt++) {
        signal?.throwIfAborted();
        const result = await sendJson(
            desktop,
            bundle.completeUrl,
            { token: bundle.form.token },
            signal,
        );
        if (result.status >= 200 && result.status < 300 && result.status !== 202) return;
        if (!isRetryable(result)) throw httpError(result);
        await retryDelay(Math.min(attempt, 4), signal, 30_000);
    }
    throw new UploadV2Error("nte_bundle_incomplete");
}

async function uploadPreparedIntent({
    desktop,
    member,
    signal,
    onProgress,
    onIntentReady,
}: {
    desktop: NahidaDesktop;
    member: PreparedUpload;
    signal?: AbortSignal;
    onProgress?: (progress: UploadProgress) => void;
    onIntentReady: (source: FinalFile, copies: FinalFile[]) => void;
}) {
    signal?.throwIfAborted();
    await uploadDirect(desktop, member.upload, member.source, member.prepared, signal, (bytes) =>
        onProgress?.({ bytes, isServerDeduplicated: false }),
    );
    onIntentReady(member.source, member.copies);
}

async function uploadPack({
    desktop,
    members,
    signal,
    onProgress,
    onIntentReady,
}: {
    desktop: NahidaDesktop;
    members: PreparedUpload[];
    signal?: AbortSignal;
    onProgress?: (progress: UploadProgress) => void;
    onIntentReady: (source: FinalFile, copies: FinalFile[]) => void;
}) {
    const pack = Buffer.concat(members.map((member) => member.prepared.data));
    const manifest = JSON.stringify({
        entries: members.map((member) => ({
            intentId: member.upload.intentId,
            token: member.upload.form.token,
            sha256: member.upload.form.sha256,
            payloadBytes: member.payloadBytes,
            ...(member.prepared.compAlg ? { compAlg: member.prepared.compAlg } : {}),
        })),
    });

    for (let attempt = 0; attempt <= RETRY_LIMIT; attempt++) {
        signal?.throwIfAborted();
        let reportedBytes = 0;
        let uploadedPayload = 0;
        const result = await sendMultipart({
            desktop,
            url: packUploadUrl(members[0].upload.url),
            method: "POST",
            fields: [["manifest", manifest]],
            file: pack,
            filename: "pack.bin",
            fileFieldName: "pack",
            signal,
            onProgress: (bytes) => {
                uploadedPayload += bytes;
                const next = logicalBytesForPackProgress(members, uploadedPayload);
                onProgress?.({ bytes: next - reportedBytes, isServerDeduplicated: false });
                reportedBytes = next;
            },
        });
        if (result.status >= 200 && result.status < 300 && result.status !== 202) {
            const packResults = (result.payload as { results?: IntentPackResult[] } | undefined)
                ?.results;
            if (!packResults) {
                onProgress?.({ bytes: -reportedBytes, isServerDeduplicated: false });
                throw new Error(result.reason ?? "pack_result_missing");
            }
            const failures: string[] = [];
            members.forEach((member, index) => {
                const packResult = packResultForIntent(packResults, member.upload.intentId);
                const credited = creditedLogicalBytesForMember(members, index, uploadedPayload);
                if (packResult?.status === "completed") {
                    if (credited < member.logicalSize) {
                        onProgress?.({
                            bytes: member.logicalSize - credited,
                            isServerDeduplicated: false,
                        });
                    }
                    onIntentReady(member.source, member.copies);
                    return;
                }
                if (credited > 0) {
                    onProgress?.({ bytes: -credited, isServerDeduplicated: false });
                }
                failures.push(
                    `${member.source.name}: ${packResult?.reason ?? packResult?.status ?? "pack_result_missing"}`,
                );
            });
            if (failures.length > 0) throw new Error(failures.join(", "));
            return;
        }
        if (reportedBytes > 0) onProgress?.({ bytes: -reportedBytes, isServerDeduplicated: false });
        if (!isRetryable(result) || attempt === RETRY_LIMIT) throw httpError(result);
        await retryDelay(attempt, signal);
    }
}

function packResultForIntent(results: IntentPackResult[], intentId: string) {
    const matches = results.filter((result) => result.intentId === intentId);
    if (matches.length !== 1) return undefined;
    return matches[0];
}

async function uploadDirect(
    desktop: NahidaDesktop,
    upload: UploadPlanEntry,
    file: FinalFile,
    prepared: { data: Buffer; compAlg?: "zstd" },
    signal?: AbortSignal,
    onProgress?: (bytes: number) => void,
) {
    for (let attempt = 0; attempt <= RETRY_LIMIT; attempt++) {
        signal?.throwIfAborted();
        let uploadedPayloadBytes = 0;
        let reportedBytes = 0;
        const result = await sendMultipart({
            desktop,
            url: upload.url,
            method: "POST",
            fields: [
                ["token", upload.form.token],
                ...(prepared.compAlg
                    ? ([["compAlg", prepared.compAlg]] as Array<[string, string]>)
                    : []),
            ],
            file: prepared.data,
            filename: file.name,
            signal,
            onProgress: (bytes) => {
                uploadedPayloadBytes += bytes;
                const target = Math.min(
                    file.size,
                    Math.floor(
                        (uploadedPayloadBytes / Math.max(1, prepared.data.byteLength)) * file.size,
                    ),
                );
                onProgress?.(target - reportedBytes);
                reportedBytes = target;
            },
        });
        if (result.status >= 200 && result.status < 300 && result.status !== 202) {
            if (reportedBytes < file.size) onProgress?.(file.size - reportedBytes);
            return;
        }
        if (reportedBytes > 0) onProgress?.(-reportedBytes);
        if (!isRetryable(result) || attempt === RETRY_LIMIT) throw httpError(result);
        await retryDelay(attempt, signal);
    }
}

function preparedPayloadLength(prepared: PreparedDirectFile) {
    return prepared.data?.byteLength ?? prepared.byteLength ?? 0;
}

async function readPreparedPart(
    prepared: PreparedDirectFile,
    index: number,
    handle?: FileHandle,
) {
    const total = preparedPayloadLength(prepared);
    const start = index * PART_SIZE;
    const size = Math.min(PART_SIZE, total - start);
    if (prepared.data) return prepared.data.subarray(start, start + size);
    if (!prepared.path) throw new Error("prepared_payload_missing");
    const dest = Buffer.allocUnsafe(size);
    const fileHandle = handle ?? (await open(prepared.path, "r"));
    try {
        const { bytesRead } = await fileHandle.read(dest, 0, size, start);
        if (bytesRead !== size) {
            throw new Error(`Unexpected EOF while reading ${prepared.path}`);
        }
        return dest;
    } finally {
        if (!handle) await fileHandle.close();
    }
}

async function uploadParts(
    desktop: NahidaDesktop,
    upload: UploadPlanEntry,
    file: FinalFile,
    prepared: PreparedDirectFile,
    signal?: AbortSignal,
    onProgress?: (bytes: number) => void,
) {
    signal?.throwIfAborted();
    const payloadLength = Math.max(1, preparedPayloadLength(prepared));
    const totalParts = Math.max(1, Math.ceil(preparedPayloadLength(prepared) / PART_SIZE));
    let uploadedPayloadBytes = 0;
    let reportedBytes = 0;
    const applyPayloadProgress = (payloadAbsolute: number) => {
        uploadedPayloadBytes = payloadAbsolute;
        const target = Math.min(
            file.size,
            Math.floor((uploadedPayloadBytes / payloadLength) * file.size),
        );
        const delta = target - reportedBytes;
        reportedBytes = target;
        if (delta !== 0) onProgress?.(delta);
    };
    const reportPayloadDelta = (delta: number) => {
        applyPayloadProgress(uploadedPayloadBytes + delta);
    };
    const completeProgress = () => {
        if (reportedBytes < file.size) {
            onProgress?.(file.size - reportedBytes);
            reportedBytes = file.size;
        }
    };
    const resetProgress = () => {
        if (reportedBytes > 0) onProgress?.(-reportedBytes);
        uploadedPayloadBytes = 0;
        reportedBytes = 0;
    };
    const sendAllParts = async () => {
        const handle =
            prepared.data || !prepared.path ? undefined : await open(prepared.path, "r");
        try {
            for (let index = 0; index < totalParts; index++) {
                signal?.throwIfAborted();
                const buffer = await readPreparedPart(prepared, index, handle);
                for (let attempt = 0; attempt <= RETRY_LIMIT; attempt++) {
                    signal?.throwIfAborted();
                    let attemptReportedBytes = 0;
                    const result = await sendMultipart({
                        desktop,
                        url: `${upload.url}/parts/${index}`,
                        method: "PUT",
                        fields: [
                            ["token", upload.form.token],
                            ["totalParts", String(totalParts)],
                        ],
                        file: buffer,
                        filename: file.name,
                        signal,
                        onProgress: (bytes) => {
                            attemptReportedBytes += bytes;
                            reportPayloadDelta(bytes);
                        },
                    });
                    if (result.payload?.status === "completed") {
                        completeProgress();
                        return true;
                    }
                    if (result.status >= 200 && result.status < 300) break;
                    if (attemptReportedBytes > 0) reportPayloadDelta(-attemptReportedBytes);
                    if (!isRetryable(result) || attempt === RETRY_LIMIT) throw httpError(result);
                    await retryDelay(attempt, signal);
                }
            }
            return false;
        } finally {
            await handle?.close();
        }
    };

    try {
        if (await sendAllParts()) return;
        const startedAt = Date.now();
        let resetAfterMissingManifest = false;
        for (let attempt = 0; Date.now() - startedAt < COMPLETE_TIMEOUT_MS; attempt++) {
            signal?.throwIfAborted();
            const result = await sendJson(
                desktop,
                `${upload.url}/complete`,
                {
                    token: upload.form.token,
                    ...(prepared.compAlg ? { compAlg: prepared.compAlg } : {}),
                },
                signal,
            );
            if (result.status >= 200 && result.status < 300 && result.status !== 202) {
                completeProgress();
                return;
            }
            if (
                !resetAfterMissingManifest &&
                (result.reason === "chunk_manifest_not_found" ||
                    result.reason === "chunks_incomplete")
            ) {
                resetAfterMissingManifest = true;
                resetProgress();
                if (await sendAllParts()) return;
                continue;
            }
            if (!isRetryable(result)) throw httpError(result);
            await retryDelay(Math.min(attempt, 4), signal, 30_000);
        }
        throw new Error("complete_timeout");
    } catch (error) {
        resetProgress();
        throw error;
    }
}

async function sendMultipart({
    desktop,
    url,
    method,
    fields,
    file,
    filename,
    fileFieldName = "file",
    signal,
    onProgress,
}: {
    desktop: NahidaDesktop;
    url: string;
    method: "POST" | "PUT";
    fields: Array<[string, string]>;
    file: Uint8Array;
    filename: string;
    fileFieldName?: string;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
}) {
    const boundary = `----nahida-desktop-${randomUUID()}`;
    const multipart = createMultipartBody(
        boundary,
        fields,
        file,
        filename,
        fileFieldName,
        onProgress,
    );
    try {
        const response = await ky(url, {
            method,
            body: multipart.body,
            headers: {
                ...(await desktop.httpService.getHeaders(url)),
                "Content-Type": `multipart/form-data; boundary=${boundary}`,
                "Content-Length": String(multipart.contentLength),
            },
            signal,
            throwHttpErrors: false,
            retry: 0,
            timeout: false,
            // @ts-expect-error - duplex is required by Node/undici for streaming request bodies.
            duplex: "half",
        });
        return parseHttpBody(
            response.status,
            response.headers.get("Content-Type"),
            new Uint8Array(await response.arrayBuffer()),
        );
    } catch (error) {
        if (signal?.aborted) throw error;
        return { status: 0, reason: "network_error" };
    }
}

async function sendJson(desktop: NahidaDesktop, url: string, body: unknown, signal?: AbortSignal) {
    try {
        const response = await ky.post(url, {
            json: body,
            headers: await desktop.httpService.getHeaders(url),
            signal,
            throwHttpErrors: false,
            retry: 0,
            timeout: false,
        });
        return parseHttpBody(
            response.status,
            response.headers.get("Content-Type"),
            new Uint8Array(await response.arrayBuffer()),
        );
    } catch (error) {
        if (signal?.aborted) throw error;
        return { status: 0, reason: "network_error" };
    }
}

function createMultipartBody(
    boundary: string,
    fields: Array<[string, string]>,
    file: Uint8Array,
    filename: string,
    fileFieldName: string,
    onProgress?: (bytes: number) => void,
) {
    const encoder = new TextEncoder();
    const escape = (value: string) =>
        value
            .replaceAll("\\", "\\\\")
            .replaceAll('"', '\\"')
            .replaceAll("\r", "")
            .replaceAll("\n", "");
    const fieldParts = fields.map(([name, value]) =>
        encoder.encode(
            `\r\n--${boundary}\r\nContent-Disposition: form-data; name="${escape(name)}"\r\n\r\n${value}`,
        ),
    );
    const header = encoder.encode(
        `--${boundary}\r\nContent-Disposition: form-data; name="${escape(fileFieldName)}"; filename="${escape(filename)}"\r\nContent-Type: application/octet-stream\r\n\r\n`,
    );
    const footer = encoder.encode(`\r\n--${boundary}--\r\n`);
    let fieldIndex = 0;
    let offset = 0;
    let headerSent = false;
    let footerSent = false;
    return {
        contentLength:
            header.byteLength +
            file.byteLength +
            fieldParts.reduce((sum, part) => sum + part.byteLength, 0) +
            footer.byteLength,
        body: new ReadableStream<Uint8Array>({
            pull(controller) {
                if (!headerSent) {
                    headerSent = true;
                    controller.enqueue(header);
                    return;
                }
                if (offset < file.byteLength) {
                    const end = Math.min(offset + UPLOAD_STREAM_CHUNK_SIZE, file.byteLength);
                    const part = file.subarray(offset, end);
                    offset = end;
                    controller.enqueue(part);
                    onProgress?.(part.byteLength);
                    return;
                }
                if (fieldIndex < fieldParts.length) {
                    controller.enqueue(fieldParts[fieldIndex++]);
                    return;
                }
                if (!footerSent) {
                    footerSent = true;
                    controller.enqueue(footer);
                    return;
                }
                controller.close();
            },
        }),
    };
}

function isRetryable(result: HttpResult) {
    return (
        result.status === 0 ||
        result.status === 202 ||
        result.status === 408 ||
        result.status === 429 ||
        result.status === 524 ||
        result.status >= 500
    );
}

function httpError(result: HttpResult) {
    const code = result.payload?.code ?? result.reason ?? `http_${result.status}`;
    return new UploadV2Error(code);
}

function apiError(value: unknown, fallback: string) {
    if (typeof value === "object" && value !== null) {
        const body = value as Record<string, unknown>;
        const code = [body.code, body.error, body.reason].find(
            (candidate): candidate is string =>
                typeof candidate === "string" && candidate.length > 0,
        );
        if (code) return new UploadV2Error(code, fallback);
    }
    if (typeof value === "string" && /^[a-z][a-z0-9_]+$/i.test(value)) {
        return new UploadV2Error(value, fallback);
    }
    return new Error(fallback);
}

function parseEventData(value: string) {
    try {
        return JSON.parse(value) as unknown;
    } catch {
        return value;
    }
}

function retryDelay(attempt: number, signal?: AbortSignal, cap = 8_000) {
    return new Promise<void>((resolve, reject) => {
        if (signal?.aborted) {
            reject(signal.reason);
            return;
        }
        const onAbort = () => {
            clearTimeout(timer);
            signal?.removeEventListener("abort", onAbort);
            reject(signal?.reason);
        };
        const timer = setTimeout(
            () => {
                signal?.removeEventListener("abort", onAbort);
                resolve();
            },
            Math.min(1000 * 2 ** attempt, cap),
        );
        signal?.addEventListener("abort", onAbort, { once: true });
        if (signal?.aborted) onAbort();
    });
}
