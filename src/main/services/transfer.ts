import { BandwidthLimiter } from "@main/lib/bandwidth-limiter";
import type { DownloadParams } from "@main/lib/download";
import { SlowChunkMonitor } from "@main/lib/slow-chunk-monitor";
import type { UploadParams } from "@main/lib/upload";
import { getAggregateTransferProgress, isOpenTransferQueueStatus } from "@shared/transfer-progress";
import type { Transfer, TransferData, TransferStatus, TransferWithoutData } from "@shared/types";
import { throttle } from "es-toolkit";

import type { NahidaDesktop } from "..";

export type TransferParams = UploadParams | DownloadParams;

export interface LocalTransfer extends Transfer {
    currentId?: string;
    abortController: AbortController;
    restartParams?: TransferParams;
    completedFileUuids?: Set<string>;
    createdOrder: number;
    sessionStartBytes: number;
    speedSamples: Array<{ timestamp: number; bytes: number }>;
    error?: string;
}

const MIB = 1024 * 1024;

export class TransferService {
    private desktop: NahidaDesktop;
    private isQueueRunning: boolean = false;
    private isPowerSaveBlockerActive: boolean = false;
    private transfers: LocalTransfer[] = [];
    private queueGroupSequence = 0;
    private transferSequence = 0;

    private throttledEmits: Map<string, () => void> = new Map();
    private runners: Map<string, () => Promise<void>> = new Map();

    public readonly downloadBandwidth = new BandwidthLimiter();
    public readonly slowChunkMonitor = new SlowChunkMonitor();

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public setDownloadBandwidthLimitMibps(mibps: number) {
        this.downloadBandwidth.setRateBps(mibps > 0 ? mibps * MIB : 0);
    }

    public async applyBandwidthLimitsFromSettings() {
        const mibps = await this.desktop.setting.transfer.getDownloadBandwidthLimitMibps();
        this.setDownloadBandwidthLimitMibps(mibps);
    }

    public async refreshPowerSaveBlock() {
        const powerSaveBlockInTransfer =
            await this.desktop.setting.general.getPowerSaveBlockInTransfer();
        const anyTransfering = this.transfers.some(
            (t) => t.status === "progress" || t.status === "preparing",
        );
        const shouldBlock = anyTransfering && powerSaveBlockInTransfer;

        if (shouldBlock && !this.isPowerSaveBlockerActive) {
            try {
                await this.desktop.lib.utils.preventAppSuspension(true);
                this.isPowerSaveBlockerActive = true;
            } catch (e) {
                this.desktop.logger.error(e, "Transfer:preventAppSuspension:start");
            }
            return;
        }

        if (!shouldBlock && this.isPowerSaveBlockerActive) {
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

        const remainingTransfers = this.transfers.filter(
            (t) => t.status !== "completed" && t.status !== "canceled",
        );
        if (remainingTransfers.length === 0) {
            mainWindow.setProgressBar(-1);
            return;
        }

        const aggregateProgress = getAggregateTransferProgress(this.transfers);
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

        const progress = aggregateProgress ?? this.getFallbackWindowProgress(remainingTransfers);
        if (progress === null) {
            mainWindow.setProgressBar(-1);
            return;
        }

        mainWindow.setProgressBar(progress / 100, { mode });
    }

    private getFallbackWindowProgress(transfers: LocalTransfer[]) {
        if (transfers.length === 0) return null;

        const totalSize = transfers.reduce((sum, transfer) => sum + transfer.totalSize, 0);
        if (totalSize > 0) {
            const transferredSize = transfers.reduce(
                (sum, transfer) =>
                    sum + Math.max(0, Math.min(transfer.transferedSize, transfer.totalSize)),
                0,
            );
            return Math.max(0, Math.min(100, (transferredSize / totalSize) * 100));
        }

        const totalProgress = transfers.reduce(
            (sum, transfer) => sum + Math.max(0, Math.min(100, transfer.progress || 0)),
            0,
        );
        return Math.max(0, Math.min(100, totalProgress / transfers.length));
    }

    private createQueueGroupId() {
        if (!this.transfers.some((transfer) => isOpenTransferQueueStatus(transfer.status))) {
            this.queueGroupSequence += 1;
        }

        return this.queueGroupSequence;
    }

    private moveTransferToCurrentQueueGroup(transfer: LocalTransfer) {
        if (isOpenTransferQueueStatus(transfer.status) && transfer.queueGroupId !== undefined) {
            return;
        }

        transfer.queueGroupId = this.createQueueGroupId();
    }

    private getSafeTransfers(newestFirst = false): TransferWithoutData[] {
        const transfers = newestFirst
            ? [...this.transfers].sort((a, b) => b.createdOrder - a.createdOrder)
            : this.transfers;

        return transfers.map((t) => {
            const {
                abortController: _abortController,
                restartParams: _restartParams,
                completedFileUuids: _completedFileUuids,
                createdOrder: _createdOrder,
                sessionStartBytes: _sessionStartBytes,
                data: _data,
                ...rest
            } = t;
            return rest;
        });
    }

    private emitUpdate(): void {
        const safeTransfers: TransferWithoutData[] = this.getSafeTransfers(true);
        this.syncMainWindowProgressBar();
        this.desktop.window.main.window?.webContents.send("transfer:update", safeTransfers);
    }

    public getAllTransfer(): TransferWithoutData[] {
        return this.getSafeTransfers();
    }

    public getDisplayTransfers(): TransferWithoutData[] {
        return this.getSafeTransfers(true);
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

    public markFileFailed(pid: string) {
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
    }: {
        pid: string;
        currentId?: string;
        type: "upload" | "download";
        data: TransferData;
        abortController: AbortController;
        name: string;
        restartParams?: TransferParams;
        initialStatus: TransferStatus;
        path?: string;
    }) {
        const totalSize = data.files.reduce((acc, cur) => acc + cur.size, 0);
        const transfer: LocalTransfer = {
            pid,
            type,
            queueGroupId: this.createQueueGroupId(),
            currentId,
            status: initialStatus,
            totalSize,
            transferedSize: 0,
            progress: 0,
            speed: 0,
            eta: 0,
            abortController,
            startTime: Date.now(),
            createdOrder: ++this.transferSequence,
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
        };

        this.transfers.push(transfer);

        this.throttledEmits.set(
            pid,
            throttle(() => this.emitUpdate(), 500),
        );

        await this.refreshPowerSaveBlock();
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

    public registerRunner(pid: string, runner: () => Promise<void>) {
        this.runners.set(pid, runner);

        const transfer = this.getTransferByPID(pid);
        if (transfer && transfer.status === "pending") {
            void this.processQueue();
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
        } finally {
            this.isQueueRunning = false;
            void this.processQueue();
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
            await this.pauseTransfer(running.pid);
        }

        const index = this.transfers.indexOf(transfer);
        if (index > -1) {
            this.transfers.splice(index, 1);
            this.transfers.unshift(transfer);
        }

        if (transfer.status !== "preparing") {
            this.moveTransferToCurrentQueueGroup(transfer);
            transfer.status = "pending";
        }

        transfer.failedFiles = 0;
        transfer.error = undefined;
        this.emitUpdate();
        void this.processQueue();
    }

    public async updateTransfer(
        pid: string,
        updates: Partial<
            Omit<LocalTransfer, "pid" | "type" | "data" | "startTime" | "createdOrder">
        >,
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
            await this.refreshPowerSaveBlock();
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

    public async cancelTransfer(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;
        if (
            transfer.status === "canceled" ||
            transfer.status === "error" ||
            transfer.status === "completed"
        ) {
            await this.removeTransfer(pid);
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
            await this.refreshPowerSaveBlock();
            this.emitUpdate();
        }
    }

    public async pauseTransfer(pid: string) {
        const transfer = this.transfers.find((t) => t.pid === pid);
        if (!transfer) return;

        if (
            transfer.status === "progress" ||
            transfer.status === "pending" ||
            transfer.status === "preparing"
        ) {
            transfer.abortController.abort();
            transfer.status = "paused";
            await this.refreshPowerSaveBlock();
            this.emitUpdate();
        }
    }

    public async removeTransfer(pid: string) {
        const index = this.transfers.findIndex((t) => t.pid === pid);
        if (index !== -1) {
            const transfer = this.transfers[index];
            if (transfer.status === "progress" || transfer.status === "preparing") {
                transfer.abortController.abort();
            }
            this.transfers.splice(index, 1);
            this.throttledEmits.delete(pid);
            this.runners.delete(pid);
            await this.refreshPowerSaveBlock();
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
}

export default TransferService;
