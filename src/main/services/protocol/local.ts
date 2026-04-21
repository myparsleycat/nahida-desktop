import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { imageCache } from "@main/internal/db/schema";
import { fileTypeFromBuffer } from "file-type";
import fse from "fs-extra";
import PQueue from "p-queue";
import sharp from "sharp";

export class LocalProtocol {
    private desktop: NahidaDesktop;
    private queue: PQueue;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.queue = new PQueue({ concurrency: 4 });
    }

    public handle = async (request: Request) => {
        const url = new URL(request.url);

        let fullPath = decodeURIComponent(url.pathname);

        if (url.host) {
            fullPath = `${url.host}:${fullPath}`;
        }

        if (fullPath.startsWith("/")) {
            fullPath = fullPath.slice(1);
        }

        const buffer = await fse.readFile(fullPath);
        const fileType = await fileTypeFromBuffer(buffer);

        const convertImageMime = ["image/jpeg", "image/png", "image/webp"];

        const isOrig = url.searchParams.get("orig") === "true";

        if (!isOrig && fileType && convertImageMime.includes(fileType.mime)) {
            const imgHash = await this.desktop.lib.utils.getFileHash(fullPath);
            const cachedImg = await this.desktop.lib.db.query.imageCache.findFirst({
                where: (t, { eq }) => eq(t.hash, imgHash),
            });

            if (cachedImg) {
                const imgArrayBuffer = new Uint8Array(cachedImg.image);
                const blob = new Blob([imgArrayBuffer], { type: "image/webp" });
                return new Response(blob);
            } else {
                const resizedImg = await this.queue.add(() =>
                    sharp(buffer)
                        .resize({ width: 500, height: 500, fit: "inside" })
                        .webp({ quality: 70 })
                        .toBuffer(),
                );

                if (!resizedImg) {
                    return new Response("not found", { status: 404 });
                }

                this.desktop.logger.info(`Resized image: ${fullPath}`, "LocalProtocol.handle");

                const blob = new Blob([new Uint8Array(resizedImg)], { type: "image/webp" });
                await this.desktop.lib.db.insert(imageCache).values({
                    hash: imgHash,
                    image: Buffer.from(resizedImg),
                    size: resizedImg.length,
                });
                return new Response(blob);
            }
        } else {
            const mimeType = resolveMimeType(fullPath, fileType);
            const totalLength = buffer.byteLength;
            const rangeHeader = request.headers.get("range");

            if (rangeHeader) {
                const match = /^bytes=(\d*)-(\d*)$/.exec(rangeHeader);
                if (!match) {
                    return new Response(null, {
                        status: 416,
                        headers: { "Content-Range": `bytes */${totalLength}` },
                    });
                }

                const start = match[1]
                    ? parseInt(match[1], 10)
                    : totalLength - parseInt(match[2], 10);
                const end = match[2] ? parseInt(match[2], 10) : totalLength - 1;

                if (isNaN(start) || isNaN(end) || start > end || start < 0 || end >= totalLength) {
                    return new Response(null, {
                        status: 416,
                        headers: { "Content-Range": `bytes */${totalLength}` },
                    });
                }

                const slice = buffer.subarray(start, end + 1);
                return new Response(slice as BodyInit, {
                    status: 206,
                    headers: {
                        "Content-Type": mimeType,
                        "Content-Length": slice.byteLength.toString(),
                        "Content-Range": `bytes ${start}-${end}/${totalLength}`,
                        "Accept-Ranges": "bytes",
                    },
                });
            }

            return new Response(buffer as BodyInit, {
                headers: {
                    "Content-Type": mimeType,
                    "Content-Length": totalLength.toString(),
                    "Accept-Ranges": "bytes",
                },
            });
        }
    };
}

function resolveMimeType(
    fullPath: string,
    fileType: Awaited<ReturnType<typeof fileTypeFromBuffer>>,
): string {
    if (fileType?.mime) {
        return fileType.mime;
    }

    const ext = path.extname(fullPath).toLowerCase();
    switch (ext) {
        case ".glb":
            return "model/gltf-binary";
        case ".gltf":
            return "model/gltf+json";
        case ".png":
            return "image/png";
        case ".jpg":
        case ".jpeg":
            return "image/jpeg";
        case ".webp":
            return "image/webp";
        case ".gif":
            return "image/gif";
        case ".bmp":
            return "image/bmp";
        case ".avif":
            return "image/avif";
        case ".mp4":
            return "video/mp4";
        case ".webm":
            return "video/webm";
        case ".mov":
            return "video/quicktime";
        case ".avi":
            return "video/x-msvideo";
        case ".mkv":
            return "video/x-matroska";
        default:
            return "application/octet-stream";
    }
}

export default LocalProtocol;
