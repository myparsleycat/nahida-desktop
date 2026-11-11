import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';

// Pin the CDN file so an update to Microsoft's Evergreen download link does not change the packaged binary.
const url =
  'https://msedge.sf.dl.delivery.mp.microsoft.com/filestreamingservice/files/068be74d-a831-494f-8f57-c173756d5a33/MicrosoftEdgeWebview2Setup.exe';
const sha256 = 'cf2f2445fef0c5c14e372045f84998771eeb8bb01d59be4afd00692fa160082e';
const destination = process.argv[2];

if (!destination) {
  throw new Error('Pass the destination path for the WebView2 bootstrapper.');
}

const matches = (bytes) => createHash('sha256').update(bytes).digest('hex') === sha256;
const cached = await readFile(destination).catch((error) => {
  if (error.code !== 'ENOENT') {
    throw error;
  }
});

if (!cached || !matches(cached)) {
  const response = await fetch(url, { signal: AbortSignal.timeout(45_000) });
  if (!response.ok) {
    throw new Error(`WebView2 bootstrapper download failed: HTTP ${response.status}`);
  }
  const bytes = Buffer.from(await response.arrayBuffer());
  if (!matches(bytes)) {
    throw new Error('WebView2 bootstrapper SHA-256 does not match the pinned value.');
  }

  await mkdir(path.dirname(destination), { recursive: true });
  const temporary = `${destination}.${randomUUID()}.tmp`;
  try {
    await writeFile(temporary, bytes);
    await rename(temporary, destination);
  } finally {
    await rm(temporary, { force: true });
  }
}
