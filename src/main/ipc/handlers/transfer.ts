import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerTransferHandlers(d: NahidaDesktop) {
    rh("transfer:list", async () => {
        return d.service.transfer.getDisplayTransfers();
    });

    rh("transfer:cancel", async (pid) => {
        return d.service.transfer.cancelTransfer(pid);
    });

    rh("transfer:pause", async (pid) => {
        await d.service.transfer.pauseTransfer(pid);
    });

    rh("transfer:resume", async (pid) => {
        d.service.drive.fn.resumeTransfer(pid);
    });

    rh("transfer:retry", async (pid) => {
        d.service.drive.fn.retryTransfer(pid);
    });

    rh("transfer:pause-all", async () => {
        const transfers = d.service.transfer.getAllTransfer();
        await Promise.all(
            transfers
                .filter((t) => t.status === "progress")
                .map((t) => d.service.transfer.pauseTransfer(t.pid)),
        );
    });

    rh("transfer:resume-all", async () => {
        const transfers = d.service.transfer.getAllTransfer();
        transfers.forEach((t) => {
            if (t.status === "paused") {
                d.service.drive.fn.resumeTransfer(t.pid);
            }
        });
    });

    rh("transfer:clear", async () => {
        const transfers = d.service.transfer.getAllTransfer();
        await Promise.all(
            transfers
                .filter(
                    (t) =>
                        t.status === "completed" ||
                        t.status === "canceled" ||
                        t.status === "error",
                )
                .map((t) => d.service.transfer.removeTransfer(t.pid)),
        );
    });
}
