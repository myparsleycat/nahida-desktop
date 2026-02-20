import { pathToFileURL } from "node:url";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { imageCache } from "@main/internal/db/schema";
import { convertImage } from "@native/image";
import { net } from "electron";
import { fileTypeFromFile } from "file-type/node";
import PQueue from "p-queue";

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

        const fileType = await fileTypeFromFile(fullPath);

        const convertImageMime = ["image/jpeg", "image/png", "image/webp"];

        const isOrig = url.searchParams.get("orig") === "true";
        const fileUrl = pathToFileURL(fullPath).href;

        if (!isOrig && fileType && convertImageMime.includes(fileType.mime)) {
            const imgHash = await this.desktop.lib.utils.getFileHash(fullPath);
            const cachedImg = await this.desktop.lib.db.query.imageCache.findFirst({
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
                await this.desktop.lib.db.insert(imageCache).values({
                    hash: imgHash,
                    image: Buffer.from(resizedImg),
                    size: resizedImg.length,
                });
                return new Response(blob);
            }
        } else {
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
