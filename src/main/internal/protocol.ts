import path from "node:path";
import type { Readable } from "node:stream";
import { fileTypeFromBuffer } from "file-type";
import fse from "fs-extra";
import type { NahidaDesktop } from "..";

export const NahidaProtocolHandler = (desktop: NahidaDesktop, req: Request) => {
    const parsedUrl = new URL(req.url);
    const hostname = parsedUrl.hostname;
    const params = parsedUrl.searchParams;

    switch (hostname) {
        case "video-local":
            return handleVideoLocal(params, req);
        case "image-local":
            return handleImageLocal(params);
        case "image-web":
            return handleImageWeb(params, desktop);
        default:
            return new Response("File not found", { status: 404 });
    }
};

function getVideoMime(ext: string) {
    if (ext === ".webm") return "video/webm";
    if (ext === ".mov") return "video/quicktime";
    if (ext === ".avi") return "video/x-msvideo";
    if (ext === ".mkv") return "video/x-matroska";
    return "video/mp4";
}

async function handleVideoLocal(params: URLSearchParams, req: Request) {
    const videoPath = params.get("path");

    if (!videoPath) {
        return new Response("path param is required", { status: 400 });
    }

    if (!(await fse.pathExists(videoPath))) {
        return new Response("Video not found", { status: 404 });
    }

    const stat = await fse.stat(videoPath);
    const fileSize = stat.size;
    const videoMime = getVideoMime(path.extname(videoPath).toLowerCase());
    const range = req.headers.get("range");

    if (!range) {
    const arrbuf = await fse.readFile(videoPath);
    return new Response(arrbuf as BodyInit, {
      headers: {
                "Content-Type": videoMime,
                "Content-Length": arrbuf.byteLength.toString(),
                "Accept-Ranges": "bytes",
            },
        });
    }

    const parts = range.replace(/bytes=/, "").split("-");
    const start = parseInt(parts[0], 10);
    const end = parts[1] ? parseInt(parts[1], 10) : fileSize - 1;
    const chunksize = end - start + 1;

    const stream = fse.createReadStream(videoPath, { start, end });
    const buffer = await streamToBuffer(stream);

  return new Response(buffer as BodyInit, {
    status: 206,
        headers: {
            "Content-Range": `bytes ${start}-${end}/${fileSize}`,
            "Accept-Ranges": "bytes",
            "Content-Length": chunksize.toString(),
            "Content-Type": videoMime,
        },
    });
}

function getImageMime(ext: string) {
    if (ext === ".png") return "image/png";
    if (ext === ".gif") return "image/gif";
    if (ext === ".webp") return "image/webp";
    if (ext === ".bmp") return "image/bmp";
    if (ext === ".avif") return "image/avif";
    return "image/jpeg";
}

async function handleImageLocal(params: URLSearchParams) {
    const imgPath = params.get("path");

    if (!imgPath) {
        return new Response("path param is required", { status: 400 });
    }

    if (!(await fse.pathExists(imgPath))) {
        return new Response("Image not found", { status: 404 });
    }

    const mimeType = getImageMime(path.extname(imgPath).toLowerCase());
  const arrbuf = await fse.readFile(imgPath);
  return new Response(arrbuf as BodyInit, {
    headers: {
            "Content-Type": mimeType,
            "Content-Length": arrbuf.byteLength.toString(),
        },
    });
}

async function handleImageWeb(params: URLSearchParams, desktop: NahidaDesktop) {
    const imgUrl = params.get("url");

    if (!imgUrl) {
        return new Response("url param is required", { status: 400 });
    }

    return fetchWithRetries(imgUrl, desktop, 3);
}

async function fetchWithRetries(
    url: string,
    desktop: NahidaDesktop,
    maxRetries: number,
    attempt = 1,
): Promise<Response> {
    try {
        const resp = await desktop.httpService.fetcher(url);

        if (!resp.ok) {
            return new Response("Error", { status: resp.status });
        }

        const arrbuf = await resp.arrayBuffer();
        const fileType = await fileTypeFromBuffer(arrbuf);
        return new Response(arrbuf, {
            headers: { "Content-Type": fileType?.mime || "" },
        });
    } catch (err) {
        if (attempt >= maxRetries) {
            const message = err instanceof Error ? err.message : String(err);
            return new Response(`Error: ${message}`, { status: 500 });
        }

        const delay = Math.pow(2, attempt - 1) * 100;
        await new Promise((resolve) => setTimeout(resolve, delay));
        return fetchWithRetries(url, desktop, maxRetries, attempt + 1);
    }
}

function streamToBuffer(stream: Readable): Promise<Buffer> {
    const chunks: Buffer[] = [];
    return new Promise((resolve, reject) => {
        stream.on("data", (chunk) => chunks.push(chunk as Buffer));
        stream.on("error", reject);
        stream.on("end", () => resolve(Buffer.concat(chunks)));
    });
}
