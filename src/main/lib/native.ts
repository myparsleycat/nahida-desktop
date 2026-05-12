import { exec } from "node:child_process";
import { promisify } from "node:util";
import { getPreviousPids, getProcessName, getTopmostPid, startTracking } from "@native/utils";
import psList from "ps-list";

const execAsync = promisify(exec);

import type { NahidaDesktop } from "..";

export class NativeLib {
    private desktop: NahidaDesktop;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async getProcessList() {
        const list = await psList();
        return list;
    }

    public getTopmostPid(pids: Array<number>): number | null {
        const pid = getTopmostPid(pids);
        return pid;
    }

    public startTracking() {
        startTracking();
    }

    public getPreviousPids(currentPid: number): number[] {
        const pids = getPreviousPids(currentPid);
        return pids;
    }

    public getProcessName(pid: number): string | null {
        const name = getProcessName(pid);
        return name;
    }
    public async getWindowTitle(pid: number): Promise<string | null> {
        try {
            const { stdout } = await execAsync(`tasklist /fi "PID eq ${pid}" /v /fo csv /nh`);
            const lines = stdout.trim().split("\r\n");
            if (lines.length === 0) return null;

            const line = lines[0];
            const parts = line.split('","');
            if (parts.length < 9) return null;

            let title = parts[parts.length - 1];
            if (title.endsWith('"')) title = title.slice(0, -1);
            if (title === "N/A") return null;

            return title;
        } catch {
            return null;
        }
    }
}
