import type { NahidaDesktop } from "@main/index";
import LocalProtocol from "@main/services/protocol/local";
import { protocol } from "electron";

export function registerLocalProtocal(desktop: NahidaDesktop) {
    const localProtocol = new LocalProtocol(desktop);
    protocol.handle("local", localProtocol.handle);
}
