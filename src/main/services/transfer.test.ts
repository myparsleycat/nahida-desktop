import { describe, expect, it, vi } from "vitest";

import type { NahidaDesktop } from "..";

import { TransferService } from "./transfer";

describe("TransferService", () => {
    it("removes optional fields when updateTransfer receives undefined", async () => {
        const desktop = {
            logger: { error: vi.fn() },
            setting: {
                general: {
                    getPowerSaveBlockInTransfer: vi.fn(async () => false),
                    getMoveTransferPageWhenStartTransfer: vi.fn(async () => false),
                },
            },
            lib: {
                utils: { preventAppSuspension: vi.fn(async () => undefined) },
            },
            window: { main: { window: null } },
            ipc: { postMessageToWindow: vi.fn() },
        } as unknown as NahidaDesktop;

        const service = new TransferService(desktop);
        const transfer = await service.createTransfer({
            pid: "upload-pid",
            type: "upload",
            data: { files: [], dirs: [] },
            abortController: new AbortController(),
            name: "Upload",
            initialStatus: "progress",
        });

        await service.updateTransfer(transfer.pid, {
            planPhase: "processing",
            planProgress: 70,
        });
        expect(service.getTransferByPID(transfer.pid)).toMatchObject({
            planPhase: "processing",
            planProgress: 70,
        });

        await service.updateTransfer(transfer.pid, {
            planPhase: undefined,
            planProgress: undefined,
        });

        const cleared = service.getTransferByPID(transfer.pid);
        expect(cleared).toBeDefined();
        expect("planPhase" in (cleared ?? {})).toBe(false);
        expect("planProgress" in (cleared ?? {})).toBe(false);
    });

    it("converts runner failures into transfer errors without rejecting the queue", async () => {
        const runnerError = new Error("download failed");
        const logger = { error: vi.fn() };
        const desktop = {
            logger,
            setting: {
                general: {
                    getPowerSaveBlockInTransfer: vi.fn(async () => false),
                    getMoveTransferPageWhenStartTransfer: vi.fn(async () => false),
                },
            },
            lib: {
                utils: { preventAppSuspension: vi.fn(async () => undefined) },
            },
            window: { main: { window: null } },
            ipc: { postMessageToWindow: vi.fn() },
        } as unknown as NahidaDesktop;

        const service = new TransferService(desktop);
        const transfer = await service.createTransfer({
            pid: "download-pid",
            type: "download",
            data: { files: [], dirs: [] },
            abortController: new AbortController(),
            name: "Download",
            initialStatus: "preparing",
        });

        service.registerRunner(transfer.pid, async () => {
            throw runnerError;
        });
        transfer.status = "pending";

        await expect(service.processQueue()).resolves.toBeUndefined();

        expect(service.getTransferByPID(transfer.pid)).toMatchObject({
            status: "error",
            error: runnerError.message,
        });
        expect(logger.error).toHaveBeenCalledWith(
            runnerError,
            `Transfer:processQueue:${transfer.pid}`,
        );
    });
});
