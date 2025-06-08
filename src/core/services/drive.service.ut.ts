import { api } from '@core/lib/fetcher';
import { DirDownloadUrl } from '@core/const';

export const GetDirDownloadWithStream = async (params: {
  id: string;
  linkId?: string;
  password?: string;
}) => {
  const { id, linkId, password } = params;

  const downloadData = {
    totalBytes: 0,
    files: [] as Array<{
      uuid: string;
      fileId: string;
      parentId: string | null;
      name: string;
      size: number;
      compAlg: "gzip" | "zstd" | null;
      url: string;
    }>,
    dirs: [] as Array<{
      uuid: string;
      parentId: string | null;
      name: string;
    }>
  };

  const queryParams = new URLSearchParams({ uuid: id });
  if (linkId) queryParams.append('linkId', linkId);
  if (password) queryParams.append('password', password);

  return new Promise<{
    totalBytes: number;
    files: Array<{
      uuid: string;
      fileId: string;
      parentId: string | null;
      name: string;
      size: number;
      compAlg: "gzip" | "zstd" | null;
      url: string;
    }>;
    dirs: Array<{
      uuid: string;
      parentId: string | null;
      name: string;
    }>;
  }>(async (resolve, reject) => {
    const url = DirDownloadUrl + `?${queryParams.toString()}`;

    let buffer = '';

    try {
      const response = await api.get(url, {
        headers: { 'Accept': 'text/event-stream' },
        responseType: 'stream'
      });

      response.data.on('data', (chunk: Buffer) => {
        const chunkString = chunk.toString();
        buffer += chunkString;

        const events = buffer.split('\n\n');
        // 마지막 조각은 불완전할 수 있으므로 버퍼에 보관
        buffer = events.pop() || '';

        for (const event of events) {
          if (!event.trim()) continue;

          const lines = event.split('\n');
          const eventType = lines[0].substring(7); // 'event: ' 제거
          const dataLine = lines.find(line => line.startsWith('data: '));

          if (!dataLine) continue;

          const dataString = dataLine.substring(6); // 'data: ' 제거

          try {
            const data = JSON.parse(dataString);

            switch (eventType) {
              case 'dirs':
                downloadData.dirs = data.map((dir: any) => ({
                  uuid: dir.uuid,
                  parentId: dir.parentId || null,
                  name: dir.name
                }));
                break;

              case 'files':
                if (data.chunk && Array.isArray(data.chunk)) {
                  downloadData.files.push(...data.chunk.map((file: any) => ({
                    uuid: file.uuid,
                    fileId: file.fileId,
                    parentId: file.parentId || null,
                    name: file.name,
                    size: file.size,
                    compAlg: file.compAlg,
                    url: file.url
                  })));
                }
                break;

              case 'metadata':
                downloadData.totalBytes = data.totalBytes || 0;
                break;

              case 'complete':
                if (data.success) {
                  resolve(downloadData);
                } else {
                  reject(new Error("Download failed"));
                }
                break;
            }
          } catch (e) {
            console.error('Error parsing event data:', e);
          }
        }
      });

      response.data.on('error', (err: Error) => {
        reject(err);
      });

      response.data.on('end', () => {
        if (downloadData.files.length > 0 || downloadData.dirs.length > 0) {
          resolve(downloadData);
        } else {
          reject(new Error("Download ended without complete event"));
        }
      });
    } catch (err) {
      reject(err);
    }
  });
};