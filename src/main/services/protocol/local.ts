import { pathToFileURL } from "node:url";
import { Worker } from "node:worker_threads";
import { is } from "@electron-toolkit/utils";
import { desktop } from "@main/index";
import { db } from "@main/internal/db";
import { imageCache } from "@main/internal/db/schema";
import imgWorker from "@main/worker/image.worker?modulePath";
import { convertImage, type ResizeOptions } from "@native/image";
import { net } from "electron";
import { fileTypeFromFile } from "file-type/node";
import PQueue from "p-queue";

export class LocalProtocol {
    private imgWorker: Worker;
    private queue: PQueue;

    constructor() {
        this.imgWorker = new Worker(imgWorker);
        this.queue = new PQueue({ concurrency: 4 });
    }

    // biome-ignore lint/correctness/noUnusedPrivateClassMembers: <>
    private async convertImageWithWorker(path: string, options: ResizeOptions) {
        return new Promise<Buffer>((resolve, reject) => {
            this.imgWorker.on(
                "message",
                (
                    message:
                        | { type: "complete"; resizedImg: Buffer }
                        | { type: "error"; error: string },
                ) => {
                    if (message.type === "complete") {
                        resolve(Buffer.from(message.resizedImg));
                    } else if (message.type === "error") {
                        reject(message.error);
                    }
                },
            );

            this.imgWorker.on("error", (error) => {
                reject(error);
            });

            this.imgWorker.postMessage({ path, options });
        });
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

        const fileType = await fileTypeFromFile(fullPath);

        const convertImageMime = ["image/jpeg", "image/png", "image/webp"];

        if (fileType && convertImageMime.includes(fileType.mime)) {
            const imgHash = await desktop.lib.utils.getFileHash(fullPath);
            const cachedImg = await db.query.imageCache.findFirst({
                where: (t, { eq }) => eq(t.hash, imgHash),
            });

            if (cachedImg) {
                const imgArrayBuffer = new Uint8Array(cachedImg.image);
                const blob = new Blob([imgArrayBuffer], { type: fileType.mime });
                return new Response(blob);
            } else {
                const resizedImg = await this.queue.add(() =>
                    convertImage(fullPath, {
                        width: 500,
                        height: 500,
                        quality: 70,
                        format: "webp",
                    }),
                );

                if (!resizedImg) {
                    return new Response("not found", { status: 404 });
                }

                if (is.dev) {
                    console.log("Resized image", fullPath);
                }

                const blob = new Blob([new Uint8Array(resizedImg)], { type: fileType.mime });
                await db.insert(imageCache).values({
                    hash: imgHash,
                    image: Buffer.from(resizedImg),
                    size: resizedImg.length,
                });
                return new Response(blob);
            }
        } else {
            const fileUrl = pathToFileURL(fullPath).href;

            try {
                const response = await net.fetch(fileUrl);
                return response;
            } catch {
                return new Response("not found", { status: 404 });
            }
        }
    };
}

export default LocalProtocol;
