import { createHash, randomUUID } from "node:crypto";
import { createReadStream, createWriteStream, openAsBlob } from "node:fs";
import os from "node:os";
import path from "node:path";
import { Readable, Writable } from "node:stream";

import {
    attachArchiveMetadata,
    createBundleManifest,
    parseResourceReferences,
    planBundles,
    resolveResourcePaths,
    writeZip64Archive,
    type BundleDiagnostic,
    type BundleManifestV1,
    type PlannedBundleV1,
} from "@nahida/sdk";
import { BACKEND_URL } from "@shared/const";
import { retry } from "es-toolkit";
import fse from "fs-extra";
import PQueue from "p-queue";
import writeFileAtomic from "write-file-atomic";

import type { NahidaDesktop } from "..";
import type { DirectoriesComponent, FilesComponent } from "./upload";

type HashedUploadFile = FilesComponent & { sha256: string };

const LEGACY_STANDALONE_EXTENSIONS = [
    ".buf",
    ".ib",
    ".vb",
    ".dds",
    ".ini",
    ".jpeg",
    ".jpg",
    ".png",
    ".webp",
    ".gif",
    ".avif",
    ".avifs",
    ".bmp",
    ".hlsl",
    ".py",
    ".json",
    ".txt",
    ".pmx",
    ".tga",
    ".spa",
    ".assets",
    ".wem",
    ".mp4",
    ".webm",
    ".blend",
    ".pck",
    ".bin",
];

export type ModBundleUploadOptions = {
    collectionId: string;
    currentId: string;
    sig?: string;
    sessionId?: string;
    files: HashedUploadFile[];
    directories: DirectoriesComponent[];
    createdDirectories?: Array<{ id: string; path: string }>;
    signal: AbortSignal;
    onDiagnostics?: (diagnostics: readonly BundleDiagnostic[]) => void;
    onInventoryPlanned?: (files: Array<{ id: string; size: number }>) => void;
    onDirectoriesCreated?: (directories: Array<{ id: string; path: string }>) => void;
    onFileComplete?: (fileId: string, size: number) => void;
};

type PlanResponse = {
    sessionId: string;
    files: Array<{
        path: string;
        size: number;
        sha256: string;
        resource: boolean;
        fileId?: string;
        storage: "standalone" | "bundle";
        state: "upload" | "owned" | "retry" | "reuse" | "wait";
    }>;
    bundles: Array<{
        key: string;
        fileIds: string[];
        ownedFileIds: string[];
    }>;
};

type ServerManifest = {
    version: 1;
    bundleId: string;
    entries: Array<{
        fileId: string;
        sha256: string;
        size: number;
        memberName: string;
        method: 0 | 8;
        paths: string[];
        dataOffset: number;
        compressedSize: number;
        crc32: number;
    }>;
};

type BundleInitResponse =
    | {
          bundleId: string;
          uploadId: string;
          mode: "put";
          url: string;
          expiresAt: string;
      }
    | {
          bundleId: string;
          uploadId: string;
          mode: "multipart";
          partSize: number;
          urls: string[];
          expiresAt: string;
      };

type PreparedArchive = {
    key: string;
    path: string;
    size: number;
    sha256: string;
    manifest: ServerManifest;
    sourceFiles: Array<{ id: string; size: number }>;
    checkpointPath: string;
    upload?: {
        initialized: BundleInitResponse;
        parts: Array<{ partNumber: number; etag: string }>;
        putCompleted?: boolean;
    };
};

export class AkashaBundleUploader {
    public constructor(private readonly desktop: NahidaDesktop) {}

    public async execute(options: ModBundleUploadOptions) {
        const sessionId = options.sessionId ?? randomUUID();
        const inventory = {
            files: options.files.map((file) => ({
                path: file.path,
                size: file.size,
                sha256: file.sha256,
            })),
        };
        const references = (
            await Promise.all(
                options.files
                    .filter((file) => file.name.toLowerCase().endsWith(".ini"))
                    .map(async (file) =>
                        parseResourceReferences(
                            await fse.readFile(file.fullPath, "utf8"),
                            file.path,
                        ),
                    ),
            )
        ).flat();
        const resolution = resolveResourcePaths(inventory, references);
        const bundlePlan = planBundles(inventory, resolution);
        options.onDiagnostics?.(bundlePlan.diagnostics);
        const fatalDiagnostics = bundlePlan.diagnostics.filter(
            (diagnostic) => diagnostic.severity === "error",
        );
        if (fatalDiagnostics.length > 0) {
            throw new Error(fatalDiagnostics.map((diagnostic) => diagnostic.message).join("\n"));
        }

        const resourcePaths = new Set(bundlePlan.references.map((reference) => reference.path));
        const uploadFiles = options.files.filter(
            (file) => resourcePaths.has(file.path) || isLegacyStandaloneFile(file.name),
        );
        options.onInventoryPlanned?.(
            uploadFiles.map((file) => ({ id: file.FID, size: file.size })),
        );
        const planBody = {
            sessionId,
            collectionId: options.collectionId,
            ...(options.sig && { sig: options.sig }),
            files: uploadFiles.map((file) => ({
                path: file.path,
                size: file.size,
                sha256: file.sha256,
                resource: resourcePaths.has(file.path),
            })),
            bundles: bundlePlan.bundles.map((bundle) => ({
                key: bundle.bundleId,
                sha256: bundle.entries.map((entry) => entry.sha256),
            })),
        };
        const plan = await this.waitForPlan(planBody, options.signal);
        const createdDirs = await this.createDirectories(options);
        const filesWithParents = uploadFiles.map((file) => {
            const separator = file.path.lastIndexOf("/");
            return {
                ...file,
                parentId:
                    createdDirs.get(separator < 0 ? "" : file.path.slice(0, separator)) ??
                    options.currentId,
            };
        });
        const filesByPath = new Map(filesWithParents.map((file) => [file.path, file]));
        const serverFilesBySha = new Map(plan.files.map((file) => [file.sha256, file]));
        const archives = await this.prepareArchives({
            plan: bundlePlan.bundles,
            serverPlan: plan,
            filesByPath,
            signal: options.signal,
            sessionId,
        });

        let completed = false;
        try {
            const uploadQueue = new PQueue({ concurrency: 2 });
            await uploadQueue.addAll(
                archives.map((archive) => async () => {
                    await this.uploadArchive({
                        archive,
                        collectionId: options.collectionId,
                        sig: options.sig,
                        sessionId,
                        signal: options.signal,
                    });
                    for (const file of archive.sourceFiles) {
                        options.onFileComplete?.(file.id, file.size);
                    }
                }),
            );

            const standaloneUploads = filesWithParents.filter(
                (file) => !serverFilesBySha.get(file.sha256)?.fileId,
            );
            await this.uploadStandaloneFiles({
                files: standaloneUploads,
                collectionId: options.collectionId,
                sig: options.sig,
                signal: options.signal,
                onFileComplete: options.onFileComplete,
            });

            const finalizedFiles = filesWithParents.flatMap((file) => {
                const serverFile = serverFilesBySha.get(file.sha256);
                if (!serverFile?.fileId) return [];
                return [
                    {
                        fileId: serverFile.fileId,
                        parentId: file.parentId,
                        name: file.name,
                        size: file.size,
                    },
                ];
            });
            if (finalizedFiles.length > 0) {
                await this.postJson(
                    "/akasha/mod/v2/finalize",
                    {
                        sessionId,
                        collectionId: options.collectionId,
                        ...(options.sig && { sig: options.sig }),
                        files: finalizedFiles,
                    },
                    options.signal,
                );
            }

            for (const file of filesWithParents) {
                const serverFile = serverFilesBySha.get(file.sha256);
                if (serverFile?.fileId && serverFile.state !== "owned") {
                    options.onFileComplete?.(file.FID, file.size);
                }
            }
            completed = true;
            return { sessionId, diagnostics: bundlePlan.diagnostics };
        } finally {
            if (completed) {
                await Promise.all(
                    archives.flatMap((archive) =>
                        [archive.path, archive.checkpointPath].map((filePath) =>
                            fse.remove(filePath).catch(() => {}),
                        ),
                    ),
                );
            }
        }
    }

    private async waitForPlan(body: Record<string, unknown>, signal: AbortSignal) {
        return retry(
            async () => {
                const plan = await this.postJson<PlanResponse>("/akasha/mod/v2/plan", body, signal);
                if (plan.files.some((file) => file.state === "retry")) {
                    throw new Error(
                        "Bundle reservation expired; retry the upload with a new session",
                    );
                }
                if (plan.files.some((file) => file.state === "wait")) {
                    throw new Error("Waiting for another bundle upload session");
                }
                return plan;
            },
            {
                retries: 150,
                delay: () => 2_000,
                shouldRetry: (error) =>
                    !signal.aborted &&
                    (error as Error).message === "Waiting for another bundle upload session",
                signal,
            },
        );
    }

    private async createDirectories(options: ModBundleUploadOptions) {
        if (options.createdDirectories) {
            return new Map(
                options.createdDirectories.map((directory) => [directory.path, directory.id]),
            );
        }
        if (options.directories.length === 0) return new Map<string, string>();
        const created = await this.postJson<Array<{ id: string; path: string }>>(
            "/akasha/mod/create_dirs",
            {
                current: options.currentId,
                collectionId: options.collectionId,
                ...(options.sig && { sig: options.sig }),
                dirs: options.directories.map((directory) => ({
                    name: directory.name,
                    path: directory.path,
                })),
            },
            options.signal,
        );
        options.onDirectoriesCreated?.(created);
        return new Map(created.map((directory) => [directory.path, directory.id]));
    }

    private async prepareArchives({
        plan,
        serverPlan,
        filesByPath,
        signal,
        sessionId,
    }: {
        plan: readonly PlannedBundleV1[];
        serverPlan: PlanResponse;
        filesByPath: Map<string, HashedUploadFile & { parentId: string }>;
        signal: AbortSignal;
        sessionId: string;
    }) {
        const serverBundles = new Map(serverPlan.bundles.map((bundle) => [bundle.key, bundle]));
        const fileIdsBySha = new Map(
            serverPlan.files
                .filter((file) => file.fileId)
                .map((file) => [file.sha256, file.fileId!]),
        );
        const archives: PreparedArchive[] = [];

        for (const bundle of plan) {
            if (signal.aborted) throw createAbortError();
            const serverBundle = serverBundles.get(bundle.bundleId);
            if (!serverBundle || serverBundle.ownedFileIds.length === 0) continue;
            const owned = new Set(serverBundle.ownedFileIds);
            const entries = bundle.entries.flatMap((entry) => {
                const fileId = fileIdsBySha.get(entry.sha256);
                return fileId && owned.has(fileId) ? [{ ...entry, fileId }] : [];
            });
            if (entries.length === 0) continue;
            const manifest = createBundleManifest({ ...bundle, entries });
            const archivePath = path.join(
                os.tmpdir(),
                `nahida-${sessionId}-${bundle.bundleId}.zip`,
            );
            const checkpointPath = `${archivePath}.json`;
            if ((await fse.pathExists(checkpointPath)) && (await fse.pathExists(archivePath))) {
                const checkpoint = (await fse.readJson(checkpointPath)) as PreparedArchive;
                if (
                    checkpoint.size === (await fse.stat(archivePath)).size &&
                    checkpoint.sha256 === (await hashFile(archivePath))
                ) {
                    archives.push(checkpoint);
                    continue;
                }
            }
            const writeResult = await writeZip64Archive(
                { writable: Writable.toWeb(createWriteStream(archivePath)) },
                manifest,
                entries.map((entry) => {
                    const source = filesByPath.get(entry.paths[0]);
                    if (!source) throw new Error(`Bundle source not found: ${entry.paths[0]}`);
                    return {
                        fileId: entry.fileId,
                        size: entry.size,
                        method: entry.method,
                        stream: () =>
                            Readable.toWeb(
                                createReadStream(source.fullPath),
                            ) as unknown as ReadableStream<Uint8Array>,
                    };
                }),
            );
            const enriched = attachArchiveMetadata(manifest, writeResult);
            const prepared = {
                key: bundle.bundleId,
                path: archivePath,
                size: (await fse.stat(archivePath)).size,
                sha256: await hashFile(archivePath),
                manifest: toServerManifest(enriched),
                sourceFiles: entries.flatMap((entry) =>
                    entry.paths.flatMap((entryPath) => {
                        const file = filesByPath.get(entryPath);
                        return file ? [{ id: file.FID, size: file.size }] : [];
                    }),
                ),
                checkpointPath,
            } satisfies PreparedArchive;
            await writeArchiveCheckpoint(prepared);
            archives.push(prepared);
        }
        return archives;
    }

    private async uploadArchive({
        archive,
        collectionId,
        sig,
        sessionId,
        signal,
    }: {
        archive: PreparedArchive;
        collectionId: string;
        sig?: string;
        sessionId: string;
        signal: AbortSignal;
    }) {
        const initialized =
            archive.upload && new Date(archive.upload.initialized.expiresAt).getTime() > Date.now()
                ? archive.upload.initialized
                : await this.postJson<BundleInitResponse>(
                      "/akasha/mod/v2/bundles/init",
                      {
                          sessionId,
                          collectionId,
                          ...(sig && { sig }),
                          archiveSha256: archive.sha256,
                          size: archive.size,
                          manifest: archive.manifest,
                      },
                      signal,
                  );
        if (!archive.upload || archive.upload.initialized.uploadId !== initialized.uploadId) {
            archive.upload = { initialized, parts: [] };
            await writeArchiveCheckpoint(archive);
        }
        const parts =
            initialized.mode === "put"
                ? await this.ensureSingleArchiveUploaded(initialized.url, archive, signal)
                : await this.uploadMultipartArchive(initialized, archive, signal);
        await this.postJson(
            `/akasha/mod/v2/bundles/${initialized.bundleId}/complete`,
            {
                sessionId,
                parts,
            },
            signal,
        );
        await retry(
            async () => {
                const status = await this.getJson<{
                    status: string;
                    bundle: { status: string };
                }>(`/akasha/mod/v2/uploads/${initialized.uploadId}`, signal);
                if (status.status === "failed" || status.bundle.status === "failed") {
                    throw new Error("Bundle verification failed");
                }
                if (status.status !== "verified" || status.bundle.status !== "verified") {
                    throw new Error("Bundle verification pending");
                }
            },
            {
                retries: 150,
                delay: () => 2_000,
                shouldRetry: (error) =>
                    !signal.aborted && (error as Error).message === "Bundle verification pending",
                signal,
            },
        );
    }

    private async ensureSingleArchiveUploaded(
        url: string,
        archive: PreparedArchive,
        signal: AbortSignal,
    ) {
        if (!archive.upload?.putCompleted) {
            await this.uploadSingleArchive(url, archive, signal);
            archive.upload!.putCompleted = true;
            await writeArchiveCheckpoint(archive);
        }
        return [];
    }

    private async uploadSingleArchive(url: string, archive: PreparedArchive, signal: AbortSignal) {
        const response = await this.desktop.httpService.fetcher(url, {
            method: "PUT",
            body: await openAsBlob(archive.path, { type: "application/zip" }),
            headers: {
                "Content-Type": "application/zip",
                "Content-Length": String(archive.size),
                "x-amz-checksum-sha256": Buffer.from(archive.sha256, "hex").toString("base64"),
            },
            signal,
        });
        if (!response.ok) throw new Error(`Bundle PUT failed: ${response.status}`);
    }

    private async uploadMultipartArchive(
        initialized: Extract<BundleInitResponse, { mode: "multipart" }>,
        archive: PreparedArchive,
        signal: AbortSignal,
    ) {
        const blob = await openAsBlob(archive.path, { type: "application/zip" });
        const queue = new PQueue({ concurrency: 2 });
        const completedParts = new Map(
            (archive.upload?.parts ?? []).map((part) => [part.partNumber, part]),
        );
        let checkpointWrite = Promise.resolve();
        await Promise.all(
            initialized.urls.map((url, index) =>
                queue.add(async () => {
                    const partNumber = index + 1;
                    if (completedParts.has(partNumber)) return;
                    const start = index * initialized.partSize;
                    const body = blob.slice(
                        start,
                        Math.min(start + initialized.partSize, archive.size),
                    );
                    const response = await this.desktop.httpService.fetcher(url, {
                        method: "PUT",
                        body,
                        headers: { "Content-Length": String(body.size) },
                        signal,
                    });
                    const etag = response.headers.get("etag");
                    if (!response.ok || !etag) throw new Error(`Bundle part ${partNumber} failed`);
                    completedParts.set(partNumber, { partNumber, etag });
                    archive.upload!.parts = [...completedParts.values()].sort(
                        (left, right) => left.partNumber - right.partNumber,
                    );
                    checkpointWrite = checkpointWrite.then(() => writeArchiveCheckpoint(archive));
                    await checkpointWrite;
                }),
            ),
        );
        return [...completedParts.values()].sort(
            (left, right) => left.partNumber - right.partNumber,
        );
    }

    private async uploadStandaloneFiles({
        files,
        collectionId,
        sig,
        signal,
        onFileComplete,
    }: {
        files: Array<HashedUploadFile & { parentId: string }>;
        collectionId: string;
        sig?: string;
        signal: AbortSignal;
        onFileComplete?: (fileId: string, size: number) => void;
    }) {
        if (files.length === 0) return;
        const groups = new Map<string, Array<HashedUploadFile & { parentId: string }>>();
        for (const file of files) {
            const group = groups.get(file.sha256) ?? [];
            group.push(file);
            groups.set(file.sha256, group);
        }
        await this.uploadStandaloneBatch({
            files: [...groups.values()].map((group) => group[0]),
            collectionId,
            sig,
            signal,
            onFileComplete,
        });
        await this.uploadStandaloneBatch({
            files: [...groups.values()].flatMap((group) => group.slice(1)),
            collectionId,
            sig,
            signal,
            onFileComplete,
        });
    }

    private async uploadStandaloneBatch({
        files,
        collectionId,
        sig,
        signal,
        onFileComplete,
    }: {
        files: Array<HashedUploadFile & { parentId: string }>;
        collectionId: string;
        sig?: string;
        signal: AbortSignal;
        onFileComplete?: (fileId: string, size: number) => void;
    }) {
        if (files.length === 0) return;
        const forms = await this.postJson<
            Array<{
                form: { name: string; size: number; sha256: string; parentId: string };
            }>
        >(
            "/akasha/mod/create_files",
            {
                collectionId,
                ...(sig && { sig }),
                files: files.map((file) => ({
                    parentId: file.parentId,
                    name: file.name,
                    path: file.path,
                    size: file.size,
                    sha256: file.sha256,
                })),
            },
            signal,
        );
        const needsUpload = new Set(forms.map((item) => item.form.sha256));
        for (const file of files.filter((item) => !needsUpload.has(item.sha256))) {
            onFileComplete?.(file.FID, file.size);
        }

        const filesBySha = new Map(files.map((file) => [file.sha256, file]));
        const queue = new PQueue({
            concurrency: Math.min(4, await this.desktop.setting.transfer.getUploadConcurrency()),
        });
        await queue.addAll(
            forms.map((item) => async () => {
                const file = filesBySha.get(item.form.sha256);
                if (!file) throw new Error(`Standalone source not found: ${item.form.sha256}`);
                const form = new FormData();
                form.set("collectionId", collectionId);
                if (sig) form.set("sig", sig);
                form.set("name", item.form.name);
                form.set("size", String(item.form.size));
                form.set("sha256", item.form.sha256);
                form.set("parentId", item.form.parentId);
                form.set("file", await openAsBlob(file.fullPath), file.name);
                const response = await this.desktop.httpService.fetcher(
                    `${BACKEND_URL}/akasha/mod/upload`,
                    {
                        method: "POST",
                        body: form,
                        headers: { "x-akasha-storage-version": "2" },
                        signal,
                    },
                );
                if (!response.ok) throw new Error(`Standalone upload failed: ${response.status}`);
                onFileComplete?.(file.FID, file.size);
            }),
        );
    }

    private async postJson<T = unknown>(pathname: string, body: unknown, signal: AbortSignal) {
        return this.requestJson<T>(pathname, {
            method: "POST",
            body: JSON.stringify(body),
            signal,
        });
    }

    private async getJson<T>(pathname: string, signal: AbortSignal) {
        return this.requestJson<T>(pathname, { method: "GET", signal });
    }

    private async requestJson<T>(pathname: string, init: RequestInit) {
        const response = await this.desktop.httpService.fetcher(`${BACKEND_URL}${pathname}`, {
            ...init,
            headers: {
                "Content-Type": "application/json",
                "x-akasha-storage-version": "2",
                ...Object.fromEntries(new Headers(init.headers).entries()),
            },
        });
        if (!response.ok) throw new Error(`${pathname} failed: ${response.status}`);
        return response.json<T>();
    }
}

function toServerManifest(manifest: BundleManifestV1): ServerManifest {
    return {
        version: 1,
        bundleId: manifest.bundleId,
        entries: manifest.entries.map((entry) => {
            if (
                entry.dataOffset === undefined ||
                entry.compressedSize === undefined ||
                entry.crc32 === undefined
            ) {
                throw new Error(`Archive metadata missing: ${entry.fileId}`);
            }
            return {
                fileId: entry.fileId,
                sha256: entry.sha256,
                size: entry.size,
                memberName: entry.fileId,
                method: entry.method === "store" ? 0 : 8,
                paths: [...entry.paths],
                dataOffset: entry.dataOffset,
                compressedSize: entry.compressedSize,
                crc32: entry.crc32,
            };
        }),
    };
}

async function hashFile(filePath: string) {
    const hash = createHash("sha256");
    for await (const chunk of createReadStream(filePath)) hash.update(chunk);
    return hash.digest("hex");
}

async function writeArchiveCheckpoint(archive: PreparedArchive) {
    await writeFileAtomic(archive.checkpointPath, JSON.stringify(archive), { mode: 0o600 });
}

function createAbortError() {
    return new DOMException("The operation was aborted", "AbortError");
}

function isLegacyStandaloneFile(name: string) {
    const loweredName = name.toLowerCase();
    return LEGACY_STANDALONE_EXTENSIONS.some((extension) => loweredName.endsWith(extension));
}
