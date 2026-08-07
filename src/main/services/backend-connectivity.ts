import { appVersion } from "@main/const";
import type { NahidaDesktop } from "@main/index";
import type { BackendStatus } from "@shared/backend";
import { BACKEND_URL } from "@shared/const";
import ky from "ky";

import { isBackendUnavailableStatus } from "./drive-errors";

const PROBE_TIMEOUT_MS = 5000;
const OFFLINE_PROBE_INTERVAL_MS = 30_000;
const ONLINE_PROBE_INTERVAL_MS = 120_000;

export class BackendConnectivity {
    private status: BackendStatus = "unknown";
    private probing = false;
    private probeTimer: ReturnType<typeof setTimeout> | null = null;

    constructor(private readonly desktop: NahidaDesktop) {}

    public getStatus() {
        return this.status;
    }

    public setOnline() {
        this.setStatus("online");
    }

    public setOffline() {
        this.setStatus("offline");
    }

    public start() {
        void this.probe();
    }

    public async probe() {
        if (this.probing) return this.status;
        this.probing = true;

        try {
            const resp = await ky.get(`${BACKEND_URL}/ping`, {
                timeout: PROBE_TIMEOUT_MS,
                retry: 0,
                throwHttpErrors: false,
                headers: {
                    "User-Agent": `Nahida Desktop/${appVersion}`,
                },
            });
            await resp.arrayBuffer();
            if (resp.status > 0 && !isBackendUnavailableStatus(resp.status)) {
                this.setOnline();
            } else {
                this.setOffline();
            }
        } catch {
            this.setOffline();
        } finally {
            this.probing = false;
            this.scheduleProbe();
        }

        return this.status;
    }

    private setStatus(next: BackendStatus) {
        if (this.status === next) return;
        this.status = next;
        this.desktop.ipc.broadcast("backend:status", next);
        if (next === "offline" && !this.probing) this.scheduleProbe();
    }

    private scheduleProbe() {
        if (this.probeTimer) clearTimeout(this.probeTimer);
        const delay =
            this.status === "offline" ? OFFLINE_PROBE_INTERVAL_MS : ONLINE_PROBE_INTERVAL_MS;
        this.probeTimer = setTimeout(() => {
            void this.probe();
        }, delay);
    }
}
