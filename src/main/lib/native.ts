import { getPreviousPids, getProcessName, getTopmostPid, startTracking } from "@native/native-util";
import { exec } from "child_process";
import psList from "ps-list";
import { promisify } from "util";

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

            // Simple CSV parse: remove leading/trailing quotes, split by ","
            // Note: Window title might contain commas, so we need careful parsing or just take the last part.
            // Tasklist CSV format: "Image Name","PID","Session Name","Session#","Mem Usage","Status","User Name","CPU Time","Window Title"
            // We can assume the last quoted string is the window title?
            // But window title can be "N/A"
            const line = lines[0];
            const parts = line.split('","');
            if (parts.length < 9) return null;

            let title = parts[parts.length - 1];
            if (title.endsWith('"')) title = title.slice(0, -1);
            if (title === "N/A") return null;

            return title;
        } catch (e) {
            return null;
        }
    }
}
