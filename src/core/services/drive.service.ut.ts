export const GetDirDownloadWithStream = async (params: {
  uuid: string;
  linkId?: string;
  password?: string;
  abortSignal: AbortSignal;
}) => {
  const { uuid, linkId, password, abortSignal } = params;

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

  const queryParams = new URLSearchParams({ uuid });
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
  }>((resolve, reject) => {
    const eventSource = new EventSource(`/api/akasha/dir/download?${queryParams.toString()}`);

    const onAbort = () => {
      eventSource.close();
      reject(new Error("Download aborted"));
    };

    if (abortSignal.aborted) {
      onAbort();
      return;
    }

    abortSignal.addEventListener('abort', onAbort);

    // eventSource.addEventListener('status', (event: MessageEvent) => {
    //   try {
    //     const data = JSON.parse(event.data);
    //     console.log('Status:', data.message);
    //   } catch (err) {
    //     console.error('파싱 오류:', err);
    //   }
    // });

    eventSource.addEventListener('dirs', (event: MessageEvent) => {
      try {
        const dirs = JSON.parse(event.data);
        downloadData.dirs = dirs;
      } catch (err) {
        console.error('디렉토리 정보 파싱 오류:', err);
      }
    });

    eventSource.addEventListener('files', (event: MessageEvent) => {
      try {
        const data = JSON.parse(event.data);
        downloadData.files = downloadData.files.concat(data.chunk);
      } catch (err) {
        console.error('파일 정보 파싱 오류:', err);
      }
    });

    eventSource.addEventListener('metadata', (event: MessageEvent) => {
      try {
        const metadata = JSON.parse(event.data);
        downloadData.totalBytes = metadata.totalBytes;
      } catch (err) {
        console.error('메타데이터 파싱 오류:', err);
      }
    });

    eventSource.addEventListener('complete', () => {
      eventSource.close();
      abortSignal.removeEventListener('abort', onAbort);
      resolve(downloadData);
    });

    eventSource.addEventListener('error', (event: MessageEvent) => {
      try {
        const data = JSON.parse(event.data);
        eventSource.close();
        abortSignal.removeEventListener('abort', onAbort);
        reject(new Error(data.message));
      } catch (err) {
        console.error('오류 이벤트 파싱 오류:', err);
        eventSource.close();
        abortSignal.removeEventListener('abort', onAbort);
        reject(err);
      }
    });

    eventSource.onerror = (error) => {
      console.error('SSE 연결 오류:', error);
      eventSource.close();
      abortSignal.removeEventListener('abort', onAbort);
      reject(new Error('서버 연결 오류가 발생했습니다'));
    };
  });
};