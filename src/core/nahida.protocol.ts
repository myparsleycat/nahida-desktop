import fs from 'node:fs';
import path from 'node:path';
import { fss } from './services';
import { fileTypeFromBuffer } from 'file-type';

export const NahidaProtocolHandler = async (req: Request) => {
  const parsedUrl = new URL(req.url);
  const hostname = parsedUrl.hostname;

  try {
    switch (hostname) {
      case 'external-image':
        const params = parsedUrl.searchParams;
        const imagePath = params.get('path');
        const imageUrl = params.get('url');

        if (!imagePath && !imageUrl) {
          return new Response('Params is required', { status: 400 });
        }

        if (imagePath) {
          if (!fs.existsSync(imagePath)) {
            return new Response('Image not found', { status: 404 });
          }

          const ext = path.extname(imagePath).toLowerCase();
          let mimeType = 'image/jpeg';
          if (ext === '.png') mimeType = 'image/png';
          else if (ext === '.gif') mimeType = 'image/gif';
          else if (ext === '.webp') mimeType = 'image/webp';
          else if (ext === '.bmp') mimeType = 'image/bmp';
          else if (ext === '.avif') mimeType = 'image/avif';

          const buf = await fss.readFile(imagePath, "buf");
          return new Response(buf, {
            headers: { 'Content-Type': mimeType }
          });
        } else if (imageUrl) {
          let lastError: any;
          const maxRetries = 3;

          for (let attempt = 1; attempt <= maxRetries; attempt++) {
            try {
              const resp = await fetch(imageUrl);

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
        }
      default:
        return new Response('File not found', { status: 404 });
    }
  } catch (e: any) {
    return new Response(`Error: ${e.message}`, { status: 500 });
  }
}