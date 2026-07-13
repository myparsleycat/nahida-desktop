import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";

import { inflateSync } from "fflate";
import fse from "fs-extra";
import { afterEach, describe, expect, it } from "vitest";

import type { NahidaDesktop } from "..";

import { AkashaBundleUploader } from "./bundle-upload";

const temporaryDirectories: string[] = [];

afterEach(async () => {
    await Promise.all(temporaryDirectories.splice(0).map((directory) => fse.remove(directory)));
});

describe("AkashaBundleUploader", () => {
    it("plans INI resources, streams a ZIP64 bundle, uploads standalone files, and finalizes", async () => {
        const directory = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-upload-test-"));
        temporaryDirectories.push(directory);
        const contents = new Map([
            ["merged.ini", Buffer.from("[ResourceTexture]\nfilename = Texture.custom\n")],
            ["Texture.custom", Buffer.from("bundle resource".repeat(10_000))],
            ["preview.png", Buffer.from("standalone preview")],
            ["ignored.exe", Buffer.from("not referenced and not allowed")],
        ]);
        await Promise.all(
            [...contents].map(([name, content]) =>
                fse.writeFile(path.join(directory, name), content),
            ),
        );
        const files = [...contents].map(([name, content], index) => ({
            FID: `local-${index}`,
            path: name,
            name,
            size: content.length,
            parentPath: "",
            fullPath: path.join(directory, name),
            sha256: sha256(content),
        }));
        const resourceFileId = "11111111-1111-4111-8111-111111111111";
        const requests: Array<{ pathname: string; body?: unknown }> = [];
        let archive = new Uint8Array();
        let serverManifest:
            | { entries: Array<{ dataOffset: number; compressedSize: number }> }
            | undefined;
        const completed = new Set<string>();
        const desktop = {
            httpService: {
                fetcher: async (url: string, init?: RequestInit) => {
                    const pathname = new URL(url).pathname;
                    if (pathname === "/bundle-put") {
                        if (!(init?.body instanceof Blob)) throw new Error("Expected bundle Blob");
                        archive = new Uint8Array(await init.body.arrayBuffer());
                        return new Response(null, { status: 200, headers: { etag: '"archive"' } });
                    }
                    if (pathname === "/akasha/mod/upload") {
                        return new Response(null, { status: 200 });
                    }

                    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
                    requests.push({ pathname, body });
                    if (pathname === "/akasha/mod/v2/plan") {
                        return Response.json({
                            sessionId: body.sessionId,
                            files: body.files.map(
                                (file: {
                                    path: string;
                                    size: number;
                                    sha256: string;
                                    resource: boolean;
                                }) => ({
                                    ...file,
                                    ...(file.resource && { fileId: resourceFileId }),
                                    storage: file.resource ? "bundle" : "standalone",
                                    state: file.resource ? "owned" : "upload",
                                }),
                            ),
                            bundles: [
                                {
                                    key: "bundle-0001",
                                    fileIds: [resourceFileId],
                                    ownedFileIds: [resourceFileId],
                                },
                            ],
                        });
                    }
                    if (pathname === "/akasha/mod/v2/bundles/init") {
                        serverManifest = body.manifest;
                        expect(body.manifest.entries[0]).toMatchObject({
                            fileId: resourceFileId,
                            memberName: resourceFileId,
                            method: 8,
                        });
                        expect(body.manifest.entries[0].dataOffset).toBeGreaterThan(0);
                        return Response.json({
                            bundleId: "bundle-server-id",
                            uploadId: "upload-id",
                            mode: "put",
                            url: "https://r2.test/bundle-put",
                            expiresAt: new Date(Date.now() + 60_000).toISOString(),
                        });
                    }
                    if (pathname === "/akasha/mod/v2/bundles/bundle-server-id/complete") {
                        return Response.json({ status: "verifying" });
                    }
                    if (pathname === "/akasha/mod/v2/uploads/upload-id") {
                        return Response.json({
                            status: "verified",
                            bundle: { status: "verified" },
                        });
                    }
                    if (pathname === "/akasha/mod/create_files") {
                        return Response.json(
                            body.files.map(
                                (file: {
                                    name: string;
                                    size: number;
                                    sha256: string;
                                    parentId: string;
                                }) => ({
                                    form: file,
                                }),
                            ),
                        );
                    }
                    if (pathname === "/akasha/mod/v2/finalize") {
                        return Response.json({ status: "finalized", files: body.files.length });
                    }
                    return new Response(null, { status: 404 });
                },
            },
            setting: { transfer: { getUploadConcurrency: async () => 2 } },
        } as unknown as NahidaDesktop;

        await new AkashaBundleUploader(desktop).execute({
            collectionId: "collection",
            currentId: "root",
            sessionId: "22222222-2222-4222-8222-222222222222",
            files,
            directories: [],
            signal: new AbortController().signal,
            onFileComplete: (fileId) => completed.add(fileId),
        });

        expect(archive.byteLength).toBeGreaterThan(0);
        const resourceEntry = serverManifest!.entries[0];
        expect(
            Buffer.from(
                inflateSync(
                    archive.subarray(
                        resourceEntry.dataOffset,
                        resourceEntry.dataOffset + resourceEntry.compressedSize,
                    ),
                ),
            ),
        ).toEqual(contents.get("Texture.custom"));
        expect(completed).toEqual(
            new Set(files.filter((file) => file.name !== "ignored.exe").map((file) => file.FID)),
        );
        const planRequest = requests.find((request) => request.pathname === "/akasha/mod/v2/plan");
        expect(planRequest).toBeDefined();
        expect((planRequest!.body as { files: Array<{ path: string }> }).files).not.toContainEqual(
            expect.objectContaining({ path: "ignored.exe" }),
        );
        expect(
            requests.find((request) => request.pathname === "/akasha/mod/v2/finalize")?.body,
        ).toMatchObject({
            files: [
                {
                    fileId: resourceFileId,
                    name: "Texture.custom",
                    parentId: "root",
                },
            ],
        });
    });
});

function sha256(data: Uint8Array) {
    return createHash("sha256").update(data).digest("hex");
}
