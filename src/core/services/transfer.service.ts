import { get, writable } from "svelte/store";
import { CompleteProcess, CurrentProcess, DownloadCurrentStatus, GamebananaCurrentStatus, ProcessStatus, QueuedProcess } from "./transfer.types";
import { nanoid } from "nanoid";





export const isTransferSheetOpen = writable(false);
export const openChangeTransferSheet = (open: boolean) => isTransferSheetOpen.set(open);

interface TransferStore {
  upload: {
    current: CurrentProcess | null;
    queue: QueuedProcess[];
    completed: CompleteProcess[];
    isProcessing: boolean;
  },
  download: {
    current: {
      pid: string;
      status: DownloadCurrentStatus
      name: string;
      downloadedSize: number
      totalSize: number;
      currentFile: number;
      totalFiles: number;
      progress: number;
      downloadBytesPerSec: number;
      download: {
        totalBytes: number;
        files: {
          uuid: string;
          fileId: string;
          parentId: string | null;
          name: string;
          size: number;
          compAlg: "gzip" | "zstd" | null;
          url: string;
        }[];
        dirs: {
          uuid: string;
          parentId: string | null;
          name: string;
        }[];
      };
      abortController: AbortController;
    } | null;
    queue: {
      pid: string;
      uuid: string;
      name: string;
      linkId?: string;
      password?: string;
      fileHandle?: FileSystemFileHandle;
    }[];
    completed: {
      pid: string;
      name: string;
      size: number;
    }[];
    isProcessing: boolean;
  },
  gamebanana: {
    current: {
      pid: string;
      status: GamebananaCurrentStatus;
      name: string;
      parentId: string;
    }[];
    completed: {
      pid: string;
      name: string;
      parentId: string;
    }[];
    isProcessing: boolean;
  },
}

class TransferService {

}

// function createTransferServer() {
//   const { subscribe, update, set } = writable<TransferStore>({
//     upload: {
//       current: null,
//       queue: [],
//       completed: [],
//       isProcessing: false
//     },
//     download: {
//       current: null,
//       queue: [],
//       completed: [],
//       isProcessing: false
//     },
//     gamebanana: {
//       current: [],
//       completed: [],
//       isProcessing: false
//     },
//   });

//   // 작업 큐
//   const uploadTaskQueue: { message: any; transferList: Transferable[] }[] = [];


//   // 업로드 상태 변경
//   const updateCurrentUploadStatus = (pid: string, status: ProcessStatus, error?: string) => {
//     update(store => {
//       if (!store.upload.current || store.upload.current.pid !== pid) {
//         return store;
//       }
//       return {
//         ...store,
//         upload: {
//           ...store.upload,
//           current: {
//             ...store.upload.current,
//             status,
//             ...(error ? { error } : {})
//           }
//         }
//       };
//     });
//   };

//   function enqueueUpload(upload: Omit<QueuedProcess, 'pid'>) {
//     const pid = nanoid();
//     const queuedUpload = { ...upload, pid };

//     // console.log('업로드 큐 추가됨', upload);

//     update(store => ({
//       ...store,
//       upload: {
//         ...store.upload,
//         queue: [...store.upload.queue, queuedUpload]
//       }
//     }));

//     const currentStore = get({ subscribe });
//     if (!currentStore.upload.isProcessing && !currentStore.upload.current) {
//       processNextUploadInQueue();
//     }

//     return pid;
//   }

//   async function processNextUploadInQueue() {
//     const storeBefore = get({ subscribe });
//     if (storeBefore.upload.isProcessing || storeBefore.upload.queue.length === 0) return;

//     const nextUpload = storeBefore.upload.queue[0];

//     update(s => ({
//       ...s,
//       upload: {
//         ...s.upload,
//         isProcessing: true,
//         current: {
//           pid: nextUpload.pid,
//           parentUUID: nextUpload.parentUUID,
//           rawFiles: nextUpload.files,
//           rawDirectories: nextUpload.directories,
//           name: nextUpload.name,
//           status: 'pending',
//           totalItems: nextUpload.totalItems,
//           processedItems: 0,
//           uploadedBytes: 0,
//           totalBytes: nextUpload.size,
//           uploadBytesPerSec: 0,
//           size: nextUpload.size,
//           files: nextUpload.files.map(file => ({
//             path: file.path,
//             name: file.name,
//             size: file.size,
//             status: 'pending'
//           })),
//           directories: nextUpload.directories.map(dir => ({
//             path: dir.path,
//             name: dir.name,
//             status: 'pending'
//           }))
//         },
//         queue: s.upload.queue.slice(1)
//       }
//     }));

//     try {
//       const storeAfter = get({ subscribe });
//       const currentUpload = storeAfter.upload.current;
//       if (!currentUpload) return;

//       // 생성해야 하는 디렉토리가 있으면 디렉토리 생성부터
//       if (currentUpload.rawDirectories && currentUpload.rawDirectories.length > 0) {
//         assignTaskToWorker({
//           action: "create_dir",
//           directories: currentUpload.rawDirectories,
//           parentUUID: currentUpload.parentUUID,
//           maxConcurrent: MAX_CONCURRENT_DIR_REQUESTS,
//           pid: currentUpload.pid,
//         }, []);
//       } else {
//         // 없다면 바로 파일 업로드
//         const dirIdMap: Record<string, string> = { "": currentUpload.parentUUID ?? "" };
//         const settings = NetworkSettingsStorage.getSettings();
//         assignTaskToWorker({
//           action: "upload_file",
//           files: currentUpload.rawFiles ?? [],
//           name: currentUpload.name,
//           parentUUID: currentUpload.parentUUID,
//           dirIdMap,
//           maxBytes: settings.uploadMaxMB * 1024 * 1024,
//           maxConnections: settings.uploadConnections,
//           pid: currentUpload.pid,
//         }, []);
//       }
//     } catch (error) {
//       console.error("Error processing upload:", error);
//       updateCurrentUploadStatus(
//         nextUpload.pid,
//         "failed",
//         error instanceof Error ? error.message : "Unknown error"
//       );
//       await completeCurrentUpload();
//     }
//   }

//   async function completeCurrentUpload() {
//     update(store => {
//       const { current, queue, completed } = store.upload;
//       if (!current) return store;

//       const completedUpload: CompleteProcess = {
//         pid: current.pid,
//         name: current.name,
//         size: current.size
//       };

//       return {
//         ...store,
//         upload: {
//           ...store.upload,
//           completed: [...completed, completedUpload],
//           isProcessing: false,
//           current: null
//         }
//       };
//     });

//     const newStore = get({ subscribe });
//     if (newStore.upload.queue.length > 0) {
//       await processNextUploadInQueue();
//     }
//   }

//   return {
//     subscribe, update, set,

//   }
// }

const tfs = new TransferService();
export { tfs };