import { safeStorage } from "electron";
import type { NahidaDesktop } from "..";

export class Crypto {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public encryptString(str: string) {
        const encryptedBuf = safeStorage.encryptString(str);
        return encryptedBuf.toString("base64");
    }

    public decryptString(base64Str: string) {
        const buffer = Buffer.from(base64Str, "base64");
        const decryptedBuf = safeStorage.decryptString(buffer);
        return decryptedBuf.toString();
    }
}

export default Crypto;
