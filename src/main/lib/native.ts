import { getPreviousPids, getProcessName, getTopmostPid, startTracking } from "@native/native-util";
import psList from "ps-list";
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
}
