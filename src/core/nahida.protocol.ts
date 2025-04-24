import fs from 'node:fs';
import path from 'node:path';
import { fss } from './services';

export const NahidaProtocolHandler = async (req: Request) => {
  const parsedUrl = new URL(req.url);
  const hostname = parsedUrl.hostname;

  try {
    switch (hostname) {
      case 'external-image':
        const params = parsedUrl.searchParams;
        const imagePath = params.get('path');

        if (!imagePath) {
          return new Response('Image path is required', { status: 400 });
        } else if (!fs.existsSync(imagePath)) {
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
      default:
        return new Response('File not found', { status: 404 });
    }
  } catch (e: any) {
    return new Response(`Error: ${e.message}`, { status: 500 });
  }
}