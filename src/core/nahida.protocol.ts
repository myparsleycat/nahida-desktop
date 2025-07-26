import fse from 'fs-extra';
import path from 'node:path';
import { FSService } from './services';
import { fileTypeFromBuffer } from 'file-type';

export const NahidaProtocolHandler = async (req: Request) => {
    const parsedUrl = new URL(req.url);
    const hostname = parsedUrl.hostname;
    const params = parsedUrl.searchParams;

    try {
        switch (hostname) {
            case 'video-local':
                const videoPath = params.get('path');

                if (!videoPath) {
                    return new Response('path param is required', { status: 400 });
                }

                if (!await fse.pathExists(videoPath)) {
                    return new Response('Video not found', { status: 404 });
                }

                const stat = await fse.stat(videoPath);
                const fileSize = stat.size;

                const videoExt = path.extname(videoPath).toLowerCase();
                let videoMime = 'video/mp4';
                if (videoExt === '.webm') videoMime = 'video/webm';
                else if (videoExt === '.mov') videoMime = 'video/quicktime';
                else if (videoExt === '.avi') videoMime = 'video/x-msvideo';
                else if (videoExt === '.mkv') videoMime = 'video/x-matroska';

                const range = req.headers.get('range');

                if (range) {
                    const parts = range.replace(/bytes=/, "").split("-");
                    const start = parseInt(parts[0], 10);
                    const end = parts[1] ? parseInt(parts[1], 10) : fileSize - 1;
                    const chunksize = (end - start) + 1;

                    const stream = fse.createReadStream(videoPath, { start, end });
                    const buffer = await streamToBuffer(stream);

                    return new Response(buffer, {
                        status: 206,
                        headers: {
                            'Content-Range': `bytes ${start}-${end}/${fileSize}`,
                            'Accept-Ranges': 'bytes',
                            'Content-Length': chunksize.toString(),
                            'Content-Type': videoMime,
                        }
                    });
                } else {
                    const videoBuf = await FSService.readFile(videoPath, 'buf');
                    return new Response(videoBuf, {
                        headers: {
                            'Content-Type': videoMime,
                            'Content-Length': videoBuf.byteLength.toString(),
                            'Accept-Ranges': 'bytes',
                        }
                    });
                }
            case 'image-local':
                const imgPath = params.get('path');

                if (!imgPath) {
                    return new Response('path param is required', { status: 400 });
                }

                if (!await (fse.pathExists(imgPath))) {
                    return new Response('Image not found', { status: 404 });
                }

                const ext = path.extname(imgPath).toLowerCase();
                let mimeType = 'image/jpeg';
                if (ext === '.png') mimeType = 'image/png';
                else if (ext === '.gif') mimeType = 'image/gif';
                else if (ext === '.webp') mimeType = 'image/webp';
                else if (ext === '.bmp') mimeType = 'image/bmp';
                else if (ext === '.avif') mimeType = 'image/avif';

                const buf = await FSService.readFile(imgPath, "buf");
                return new Response(buf, {
                    headers: {
                        'Content-Type': mimeType,
                        'content-length': buf.byteLength.toString()
                    }
                });
            case 'image-web':
                const imgUrl = params.get('url');

                if (!imgUrl) {
                    return new Response('url param is required', { status: 400 });
                }

                let lastError: any;
                const maxRetries = 3;

                for (let attempt = 1; attempt <= maxRetries; attempt++) {
                    try {
                        const resp = await fetch(imgUrl);

                        if (!resp.ok) {
                            return new Response(`Error`, { status: resp.status });
                        }

                        const arrbuf = await resp.arrayBuffer();
                        const fileType = await fileTypeFromBuffer(arrbuf);
                        return new Response(arrbuf, {
                            headers: { 'Content-Type': fileType?.mime || '' }
                        });

                    } catch (err: any) {
                        lastError = err;

                        if (attempt < maxRetries) {
                            const delay = Math.pow(2, attempt - 1) * 100;
                            await new Promise(resolve => setTimeout(resolve, delay));
                        }
                    }
                }

                return new Response(`Error: ${lastError}`, { status: 500 });
            default:
                return new Response('File not found', { status: 404 });
        }
    } catch (e: any) {
        return new Response(`Error: ${e.message}`, { status: 500 });
    }
}

async function streamToBuffer(stream: any): Promise<Buffer> {
    const chunks: any[] = [];
    return new Promise((resolve, reject) => {
        stream.on('data', (chunk: any) => chunks.push(chunk));
        stream.on('error', reject);
        stream.on('end', () => resolve(Buffer.concat(chunks)));
    });
}