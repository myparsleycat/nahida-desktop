import { desktop, NahidaDesktop } from "@main/index";
import { net, protocol } from "electron";
import { pathToFileURL } from "node:url";
import { fileTypeFromFile } from "file-type/node";
import { db } from "@main/internal/db";
import { convertImage } from "@native/image";
import { imageCache } from "@main/internal/db/schema";

export class LocalProtocol {
    // private readonly desktop: NahidaDesktop;

    // constructor(desktop: NahidaDesktop) {
    //     this.desktop = desktop;
    // }

    public async handle(request: Request) {
        const url = new URL(request.url);

        let fullPath = decodeURIComponent(url.pathname);

        if (url.host) {
            fullPath = url.host + ":" + fullPath;
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
                const resizedImg = convertImage(fullPath, {
                    width: 500,
                    height: 500,
                    quality: 70,
                    format: "webp",
                });
                if (!resizedImg) {
                    return new Response("not found", { status: 404 });
                }

                const blob = new Blob([new Uint8Array(resizedImg)], { type: fileType.mime });
                await db.insert(imageCache).values({
                    hash: imgHash,
                    image: resizedImg,
                    size: resizedImg.length,
                });
                return new Response(blob);
            }
        } else {
            const fileUrl = pathToFileURL(fullPath).href;

            try {
                const response = await net.fetch(fileUrl);
                return response;
            } catch (error) {
                return new Response("not found", { status: 404 });
            }
        }
    }
}

export default LocalProtocol;
