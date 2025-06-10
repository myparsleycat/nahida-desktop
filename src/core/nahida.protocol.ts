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
                    headers: { 'Content-Type': mimeType }
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