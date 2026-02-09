import { powerSaveBlocker } from "electron";
import { NahidaDesktop } from "..";
import fse from "fs-extra";
import crypto from "crypto";

export class Utils {
    private desktop: NahidaDesktop;
    private isPreventAppSuspension: boolean = false;
    private preventAppSuspensionId: number | null = null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async preventAppSuspension(v: boolean) {
        if (v && !this.isPreventAppSuspension) {
            const id = powerSaveBlocker.start("prevent-app-suspension");
            this.isPreventAppSuspension = true;
            this.preventAppSuspensionId = id;
            return id;
        } else if (!v && this.preventAppSuspensionId) {
            powerSaveBlocker.stop(this.preventAppSuspensionId);
            this.isPreventAppSuspension = false;
            this.preventAppSuspensionId = null;
            return null;
        } else {
            throw new Error("Invalid arguments");
        }
    }

    public async getFileHash(path: string) {
        const file = await fse.readFile(path);
        return crypto.createHash("sha256").update(file).digest("hex");
    }
}

export default Utils;
