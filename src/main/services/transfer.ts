import { NativeFolderLock } from "@native/native-fs";
import type { Transfer, TransferData, TransferStatus } from "@shared/types.gen";
import { throttle } from "es-toolkit";
import type { NahidaDesktop } from "..";

export interface LocalTransfer extends Transfer {
    currentId?: string;
    abortController: AbortController;
    restartParams?: any;
    completedFileUuids?: Set<string>;
    sessionStartBytes: number;
    speedSamples: Array<{ timestamp: number; bytes: number }>;
    error?: string;
    lockPaths?: string[];
}

export class TransferService {
    private desktop: NahidaDesktop;
    private isQueueRunning: boolean = false;
    private isPowerSaveBlockerActive: boolean = false;
    private transfers: LocalTransfer[] = [];

    private throttledEmits: Map<string, () => void> = new Map();
    private runners: Map<string, () => Promise<void>> = new Map();
    private folderLocks: Map<string, NativeFolderLock[]> = new Map();

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private async checkSettingAndChangePowerSaveBlock() {
        const powerSaveBlockInTransfer =
            await this.desktop.setting.general.getPowerSaveBlockInTransfer();
        const anyTransfering = this.transfers.some(
            (t) => t.status === "progress" || t.status === "preparing",
        );

        if (anyTransfering && powerSaveBlockInTransfer) {
            if (!this.isPowerSaveBlockerActive) {
                try {
                    await this.desktop.lib.utils.preventAppSuspension(true);
                    this.isPowerSaveBlockerActive = true;
                } catch (e) {
                    this.desktop.logger.error(e, "Transfer:preventAppSuspension:start");
                }
            }
        } else if (!anyTransfering && this.isPowerSaveBlockerActive) {
            try {
                await this.desktop.lib.utils.preventAppSuspension(false);
                this.isPowerSaveBlockerActive = false;
            } catch (e) {
                this.desktop.logger.error(e, "Transfer:preventAppSuspension:stop");
            }
        }
    }

    private syncMainWindowProgressBar() {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) return;

        const queueStatuses: TransferStatus[] = [
            "completed",
            "progress",
            "preparing",
            "pending",
            "paused",
            "error",
        ];
        const remainingTransfers = this.transfers.filter(
            (t) => t.status !== "completed" && t.status !== "canceled",
        );
        if (remainingTransfers.length === 0) {
            mainWindow.setProgressBar(-1);
            return;
        }

        const queueTransfers = this.transfers.filter((t) => queueStatuses.includes(t.status));
        if (queueTransfers.length === 0) {
            mainWindow.setProgressBar(-1);
            return;
        }

        const totalProgress = queueTransfers.reduce((sum, transfer) => {
            if (transfer.status === "completed") return sum + 100;
            return sum + Math.min(100, transfer.progress || 0);
        }, 0);

        let mode: "normal" | "indeterminate" | "error" | "paused" = "normal";

        if (remainingTransfers.some((t) => t.status === "progress")) {
            mode = "normal";
        } else if (
            remainingTransfers.some((t) => t.status === "preparing" || t.status === "pending")
        ) {
            mode = "indeterminate";
        } else if (remainingTransfers.some((t) => t.status === "paused")) {
            mode = "paused";
        } else if (remainingTransfers.some((t) => t.status === "error")) {
            mode = "error";
        }

        mainWindow.setProgressBar(totalProgress / (queueTransfers.length * 100), { mode });
    }

    private emitUpdate() {
        const safeTransfers = this.transfers.map((t) => {
            const {
                abortController,
                restartParams,
                completedFileUuids,
                sessionStartBytes,
                data,
                ...rest
            } = t;
            return rest;
        });
        this.syncMainWindowProgressBar();
        this.desktop.window.main.window?.webContents.send("transfer:update", safeTransfers);
    }

    public getAllTransfer() {
        return this.transfers.map((t) => {
            const { abortController, restartParams, sessionStartBytes, data, ...rest } = t;
            return rest;
        });
    }

    public getTransferByPID(pid: string) {
        return this.transfers.find((t) => t.pid === pid);
    }

    public markFileCompleted(pid: string, fileUuid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;

        if (!transfer.completedFileUuids) {
            transfer.completedFileUuids = new Set();
        }
        transfer.completedFileUuids.add(fileUuid);
    }

    public isFileCompleted(pid: string, fileUuid: string): boolean {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer || !transfer.completedFileUuids) return false;
        return transfer.completedFileUuids.has(fileUuid);
    }

    public getCompletedFilesCount(pid: string): number {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer || !transfer.completedFileUuids) return 0;
        return transfer.completedFileUuids.size;
    }

    public markFileFailed(pid: string, fileUuid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;

        transfer.failedFiles = (transfer.failedFiles || 0) + 1;
        this.emitUpdate();
    }

    public async createTransfer({
        pid,
        currentId,
        type,
        data,
        abortController,
        name,
        restartParams,
        initialStatus,
        path,
        lockPaths,
    }: {
        pid: string;
        currentId?: string;
        type: "upload" | "download";
        data: TransferData;
        abortController: AbortController;
        name: string;
        restartParams?: any;
        initialStatus: TransferStatus;
        path?: string;
        lockPaths?: string[];
    }) {
        const totalSize = data.files.reduce((acc, cur) => acc + cur.size, 0);
        const transfer: LocalTransfer = {
            pid,
            type,
            currentId,
            status: initialStatus,
            totalSize,
            transferedSize: 0,
            progress: 0,
            speed: 0,
            eta: 0,
            abortController,
            startTime: Date.now(),
            sessionStartBytes: 0,
            speedSamples: [],
            data,
            name,
            totalFiles: data.files.length,
            transferedFiles: 0,
            failedFiles: 0,
            restartParams,
            completedFileUuids: new Set(),
            path,
            lockPaths: this.normalizeLockPaths(lockPaths),
        };

        this.transfers.push(transfer);

        this.throttledEmits.set(
            pid,
            throttle(() => this.emitUpdate(), 500),
        );

        try {
            if (transfer.lockPaths?.length) {
                await this.acquireTransferLocks(pid, transfer.lockPaths);
            }
        } catch (error) {
            this.transfers = this.transfers.filter((t) => t.pid !== pid);
            this.throttledEmits.delete(pid);
            this.runners.delete(pid);
            throw error;
        }

        this.checkSettingAndChangePowerSaveBlock();
        this.emitUpdate();

        if (this.desktop.window.main.window) {
            this.desktop.ipc.postMessageToWindow(
                this.desktop.window.main.window,
                "fn:toast",
                "전송이 시작되었습니다",
                {
                    description: name,
                },
            );

            const moveTransferPageWhenStartTransfer =
                await this.desktop.setting.general.getMoveTransferPageWhenStartTransfer();
            if (moveTransferPageWhenStartTransfer) {
                this.desktop.ipc.postMessageToWindow(
                    this.desktop.window.main.window,
                    "fn:navi",
                    "/transfer",
                );
            }
        }

        return transfer;
    }

    public setTransferLockPaths(pid: string, lockPaths?: string[]) {
        const transfer = this.getTransferByPID(pid);
        if (!transfer) return;
        transfer.lockPaths = this.normalizeLockPaths(lockPaths);
    }

    public async acquireTransferLocks(pid: string, explicitPaths?: string[]) {
        const transfer = this.getTransferByPID(pid);
        if (!transfer) return;

        const lockPaths = this.normalizeLockPaths(explicitPaths ?? transfer.lockPaths);
        if (!lockPaths.length) return;

        this.releaseTransferLocks(pid);

        const acquired: NativeFolderLock[] = [];
        try {
            for (const lockPath of lockPaths) {
                const lock = new NativeFolderLock(lockPath);
                lock.lock();
                acquired.push(lock);
            }
            this.folderLocks.set(pid, acquired);
            transfer.lockPaths = lockPaths;
        } catch (error) {
            for (const lock of acquired) {
                try {
                    lock.unlock();
                } catch {}
            }
            throw error;
        }
    }

    public releaseTransferLocks(pid: string) {
        const locks = this.folderLocks.get(pid);
        if (!locks) return;

        for (const lock of locks) {
            try {
                lock.unlock();
            } catch (error) {
                this.desktop.logger.error(error, `Transfer:releaseTransferLocks:${pid}`);
            }
        }

        this.folderLocks.delete(pid);
    }

    public registerRunner(pid: string, runner: () => Promise<void>) {
        this.runners.set(pid, runner);

        const transfer = this.getTransferByPID(pid);
        if (transfer && transfer.status === "pending") {
            this.processQueue();
        }
    }

    public async processQueue() {
        if (this.isQueueRunning) return;

        const nextTransfer = this.transfers.find((t) => t.status === "pending");
        if (!nextTransfer) return;

        const runner = this.runners.get(nextTransfer.pid);
        if (!runner) return;

        this.isQueueRunning = true;

        try {
            await runner();
        } catch (error) {
            // runner에서 처리함
        } finally {
            this.isQueueRunning = false;
            this.processQueue();
        }
    }

    public async manualStart(pid: string) {
        const transfer = this.getTransferByPID(pid);
        if (!transfer) return;
        if (transfer.status === "progress") return;

        const running = this.transfers.find(
            (t) => t.status === "progress" || t.status === "preparing",
        );
        if (running && running.pid !== pid) {
            this.pauseTransfer(running.pid);
        }

        const index = this.transfers.indexOf(transfer);
        if (index > -1) {
            this.transfers.splice(index, 1);
            this.transfers.unshift(transfer);
        }

        if (transfer.status !== "preparing") {
            transfer.status = "pending";
        }

        if (transfer.type === "upload" && transfer.lockPaths?.length) {
            await this.acquireTransferLocks(pid, transfer.lockPaths);
        }

        transfer.failedFiles = 0;
        transfer.error = undefined;
        this.emitUpdate();
        this.processQueue();
    }

    public updateTransfer(
        pid: string,
        updates: Partial<Omit<LocalTransfer, "pid" | "type" | "data" | "startTime">>,
    ) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;

        Object.assign(transfer, updates);

        if (updates.transferedSize !== undefined && transfer.status === "progress") {
            const now = Date.now();
            const lastSample = transfer.speedSamples[transfer.speedSamples.length - 1];

            if (!lastSample || lastSample.bytes !== transfer.transferedSize) {
                transfer.speedSamples.push({
                    timestamp: now,
                    bytes: transfer.transferedSize,
                });
            }

            const SAMPLE_WINDOW_MS = 5000; // 5 seconds
            const cutoffTime = now - SAMPLE_WINDOW_MS;
            transfer.speedSamples = transfer.speedSamples.filter(
                (sample) => sample.timestamp >= cutoffTime,
            );

            if (transfer.speedSamples.length >= 2) {
                const oldestSample = transfer.speedSamples[0];
                const newestSample = transfer.speedSamples[transfer.speedSamples.length - 1];

                const timeDiff = (newestSample.timestamp - oldestSample.timestamp) / 1000;
                const bytesDiff = newestSample.bytes - oldestSample.bytes;

                if (timeDiff > 0) {
                    transfer.speed = bytesDiff / timeDiff;

                    if (transfer.speed > 0) {
                        const remaining = transfer.totalSize - transfer.transferedSize;
                        transfer.eta = Math.ceil(remaining / transfer.speed);
                    }
                }
            } else {
                transfer.speed = 0;
                transfer.eta = 0;
            }

            transfer.progress = Math.min(100, (transfer.transferedSize / transfer.totalSize) * 100);
        }

        if (updates.status) {
            if (
                updates.status === "completed" ||
                updates.status === "paused" ||
                updates.status === "canceled" ||
                updates.status === "error"
            ) {
                this.releaseTransferLocks(pid);
            }
            this.checkSettingAndChangePowerSaveBlock();
            this.emitUpdate();
        } else {
            const throttledEmit = this.throttledEmits.get(pid);
            if (throttledEmit) {
                throttledEmit();
            } else {
                this.emitUpdate();
            }
        }
    }

    public cancelTransfer(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;
        if (
            transfer.status === "canceled" ||
            transfer.status === "error" ||
            transfer.status === "completed"
        ) {
            this.removeTransfer(pid);
            return;
        }

        if (
            transfer.status === "pending" ||
            transfer.status === "progress" ||
            transfer.status === "paused" ||
            transfer.status === "preparing"
        ) {
            transfer.abortController.abort();
            transfer.status = "canceled";
            this.releaseTransferLocks(pid);
            this.checkSettingAndChangePowerSaveBlock();
            this.emitUpdate();
        }
    }

    public pauseTransfer(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;

        if (
            transfer.status === "progress" ||
            transfer.status === "pending" ||
            transfer.status === "preparing"
        ) {
            transfer.abortController.abort();
            transfer.status = "paused";
            this.releaseTransferLocks(pid);
            this.checkSettingAndChangePowerSaveBlock();
            this.emitUpdate();
        }
    }

    public removeTransfer(pid: string) {
        const index = this.transfers.findIndex((t) => t.pid === pid);
        if (index !== -1) {
            const transfer = this.transfers[index];
            if (transfer.status === "progress" || transfer.status === "preparing") {
                transfer.abortController.abort();
            }
            this.releaseTransferLocks(pid);
            this.transfers.splice(index, 1);
            this.throttledEmits.delete(pid);
            this.runners.delete(pid);
            this.checkSettingAndChangePowerSaveBlock();
            this.emitUpdate();
        }
    }

    public updateAbortController(pid: string, controller: AbortController) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (transfer) {
            transfer.abortController = controller;
        }
    }

    public resetStartTime(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (transfer) {
            transfer.startTime = Date.now();
            transfer.sessionStartBytes = transfer.transferedSize;
        }
    }

    public resetTransfer(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (transfer) {
            transfer.transferedSize = 0;
            transfer.progress = 0;
            transfer.speed = 0;
            transfer.eta = 0;
            transfer.startTime = Date.now();
            transfer.sessionStartBytes = 0;
            transfer.speedSamples = [];
            transfer.transferedFiles = 0;
            transfer.failedFiles = 0;
            transfer.error = undefined;
            this.emitUpdate();
        }
    }

    private normalizeLockPaths(paths?: string[]) {
        if (!paths?.length) return [];

        const normalized = paths
            .map((p) => p.trim())
            .filter(Boolean)
            .map((p) => p.replaceAll("/", "\\").replace(/[\\]+$/, ""))
            .sort((a, b) => a.length - b.length);

        const unique: string[] = [];
        for (const candidate of normalized) {
            const lowerCandidate = candidate.toLowerCase();
            const isNested = unique.some((existing) => {
                const lowerExisting = existing.toLowerCase();
                return (
                    lowerCandidate === lowerExisting ||
                    lowerCandidate.startsWith(`${lowerExisting}\\`)
                );
            });

            if (!isNested) {
                unique.push(candidate);
            }
        }

        return unique;
    }
}

export default TransferService;
