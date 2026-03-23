import { pathToFileURL } from "node:url";
import type { NahidaDesktop } from "@main/index";
import { imageCache } from "@main/internal/db/schema";
import { net } from "electron";
import { fileTypeFromBuffer } from "file-type/node";
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
            const fileUrl = pathToFileURL(fullPath).href;

            try {
                return await net.fetch(fileUrl);
            } catch {
                return new Response("not found", { status: 404 });
            }
        }
    };
}

export default LocalProtocol;
