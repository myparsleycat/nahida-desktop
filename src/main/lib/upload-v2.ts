import { randomUUID } from "node:crypto";
import { open } from "node:fs/promises";

import { networkFetch } from "@main/internal/network-fetch";
import { BACKEND_URL } from "@shared/const";
import type { PlanPhase } from "@shared/types";
import { chunk } from "es-toolkit";
import ky from "ky";
import type PQueue from "p-queue";
import { parseServerSentEvents } from "parse-sse";

import type { NahidaDesktop } from "..";
import type { FinalFile, UploadProgress } from "./upload";

const PLAN_PAGE_SIZE = 500;
const DIRECT_UPLOAD_THRESHOLD = 80 * 1024 * 1024;
const PART_SIZE = 25 * 1024 * 1024;
const RETRY_LIMIT = 3;
const COMPLETE_TIMEOUT_MS = 15 * 60 * 1000;
const UPLOAD_STREAM_CHUNK_SIZE = 64 * 1024;

type UploadPlanItem = {
    clientId: string;
    status: "created" | "pending" | "exists" | "denied" | "error";
    reason?: string;
    itemId?: string;
    intentId?: string;
};

type UploadPlanEntry = {
    intentId: string;
    url: string;
    method: "POST";
    form: { token: string; sha256: string };
};

type HttpResult = {
    status: number;
    reason?: string;
    payload?: { status?: string };
};

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
    prepareDirectFile: (file: FinalFile) => Promise<{ data: Buffer; compAlg?: "zstd" }>;
    onProgress?: (progress: UploadProgress) => void;
    onPlanProgress?: (progress: { phase: PlanPhase; processed: number; total: number }) => void;
    onPlanComplete?: () => void;
    signal?: AbortSignal;
}) {
    const items: UploadPlanItem[] = [];
    const uploads = new Map<string, UploadPlanEntry>();

    const url = `${BACKEND_URL}/akasha/v2/sse/drive/files:plan`;
    const pages = chunk(files, PLAN_PAGE_SIZE);
    for (const [pageIndex, page] of pages.entries()) {
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
            const body = await response.text().catch(() => response.statusText);
            throw new Error(`[upload plan failed] ${body}`);
        }
        if (!response.body) {
            throw new Error("[upload plan failed] empty response stream");
        }

        for await (const event of parseServerSentEvents(response)) {
            if (event.type === "error") {
                throw new Error(`[upload plan failed] ${event.data}`);
            }
            if (event.type === "progress") {
                const data = JSON.parse(event.data) as {
                    phase: PlanPhase;
                    processed: number;
                    total: number;
                };
                onPlanProgress?.({
                    phase: data.phase,
                    processed: pageIndex * PLAN_PAGE_SIZE + data.processed,
                    total: files.length,
                });
            }
            if (event.type === "complete") {
                const data = JSON.parse(event.data) as {
                    items: UploadPlanItem[];
                    uploads: UploadPlanEntry[];
                };
                items.push(...data.items);
                data.uploads.forEach((upload) => uploads.set(upload.intentId, upload));
            }
        }
    }

    const filesById = new Map(files.map((file) => [file.FID, file]));
    const pendingByIntent = new Map<string, FinalFile[]>();
    const rejected: UploadPlanItem[] = [];
    const returnedClientIds = new Set(items.map((item) => item.clientId));

    for (const item of items) {
        const file = filesById.get(item.clientId);
        if (!file) continue;
        if (item.status === "created" || item.status === "exists") {
            onProgress?.({ bytes: file.size, fileId: file.FID, isServerDeduplicated: true });
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
    [...pendingByIntent.entries()].forEach(([intentId, targets]) => {
        void queue.add(async () => {
            const upload = uploads.get(intentId);
            if (!upload) {
                failures.push(new Error(`Upload intent missing for ${targets[0].name}`));
                return;
            }
            try {
                const source = targets[0];
                await uploadIntent({
                    desktop,
                    upload,
                    file: source,
                    prepareDirectFile,
                    signal,
                    onProgress: (bytes) => onProgress?.({ bytes, isServerDeduplicated: false }),
                });
                onProgress?.({ bytes: 0, fileId: source.FID, isServerDeduplicated: false });
                targets.slice(1).forEach((file) =>
                    onProgress?.({
                        bytes: file.size,
                        fileId: file.FID,
                        isServerDeduplicated: true,
                    }),
                );
            } catch (error) {
                if (!signal?.aborted) {
                    desktop.logger.error(error, `UploadV2:intent:${intentId}`);
                    failures.push(error instanceof Error ? error : new Error(String(error)));
                }
            }
        });
    });
    await queue.onIdle();

    if (rejected.length > 0) {
        failures.push(
            new Error(
                rejected
                    .map(
                        (item) =>
                            `${filesById.get(item.clientId)?.name ?? item.clientId}: ${item.reason ?? item.status}`,
                    )
                    .join(", "),
            ),
        );
    }
    if (failures.length > 0) throw new Error(failures.map((error) => error.message).join("\n"));
}

async function uploadIntent({
    desktop,
    upload,
    file,
    prepareDirectFile,
    signal,
    onProgress,
}: {
    desktop: NahidaDesktop;
    upload: UploadPlanEntry;
    file: FinalFile;
    prepareDirectFile: (file: FinalFile) => Promise<{ data: Buffer; compAlg?: "zstd" }>;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
}) {
    signal?.throwIfAborted();
    if (file.size < DIRECT_UPLOAD_THRESHOLD) {
        const prepared = await prepareDirectFile(file);
        await uploadDirect(desktop, upload, file, prepared, signal, onProgress);
        return;
    }
    await uploadParts(desktop, upload, file, signal, onProgress);
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

async function uploadParts(
    desktop: NahidaDesktop,
    upload: UploadPlanEntry,
    file: FinalFile,
    signal?: AbortSignal,
    onProgress?: (bytes: number) => void,
) {
    const totalParts = Math.ceil(file.size / PART_SIZE);
    let reportedBytes = 0;
    const reportProgress = (bytes: number) => {
        reportedBytes += bytes;
        onProgress?.(bytes);
    };
    const completeProgress = () => {
        if (reportedBytes < file.size) reportProgress(file.size - reportedBytes);
    };
    const sendAllParts = async () => {
        const handle = await open(file.fullPath, "r");
        try {
            for (let index = 0; index < totalParts; index++) {
                const start = index * PART_SIZE;
                const size = Math.min(PART_SIZE, file.size - start);
                const buffer = Buffer.allocUnsafe(size);
                let bytesRead = 0;
                while (bytesRead < size) {
                    const result = await handle.read(
                        buffer,
                        bytesRead,
                        size - bytesRead,
                        start + bytesRead,
                    );
                    if (result.bytesRead === 0) {
                        throw new Error(`Unexpected EOF while reading ${file.name}`);
                    }
                    bytesRead += result.bytesRead;
                }
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
                            reportProgress(bytes);
                        },
                    });
                    if (result.payload?.status === "completed") {
                        completeProgress();
                        return true;
                    }
                    if (result.status >= 200 && result.status < 300) break;
                    if (attemptReportedBytes > 0) reportProgress(-attemptReportedBytes);
                    if (!isRetryable(result) || attempt === RETRY_LIMIT) throw httpError(result);
                    await retryDelay(attempt, signal);
                }
            }
        } finally {
            await handle.close();
        }
        return false;
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
                reportProgress(-reportedBytes);
                if (await sendAllParts()) return;
                continue;
            }
            if (!isRetryable(result)) throw httpError(result);
            await retryDelay(Math.min(attempt, 4), signal, 30_000);
        }
        throw new Error("complete_timeout");
    } catch (error) {
        if (reportedBytes > 0) reportProgress(-reportedBytes);
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
    signal,
    onProgress,
}: {
    desktop: NahidaDesktop;
    url: string;
    method: "POST" | "PUT";
    fields: Array<[string, string]>;
    file: Uint8Array;
    filename: string;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
}) {
    const boundary = `----nahida-desktop-${randomUUID()}`;
    const multipart = createMultipartBody(boundary, fields, file, filename, onProgress);
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
        return parseHttpResult(response.status, await response.text());
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
        return parseHttpResult(response.status, await response.text());
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
        `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${escape(filename)}"\r\nContent-Type: application/octet-stream\r\n\r\n`,
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

function parseHttpResult(status: number, raw: string): HttpResult {
    if (!raw) return { status };
    try {
        const value: unknown = JSON.parse(raw);
        if (typeof value === "string") return { status, reason: value };
        if (value && typeof value === "object") {
            const payload = value as { status?: string; reason?: string; message?: string };
            return { status, payload, reason: payload.reason ?? payload.message };
        }
    } catch {
        return { status, reason: raw.slice(0, 200) };
    }
    return { status };
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
    return new Error(result.reason ?? `http_${result.status}`);
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
