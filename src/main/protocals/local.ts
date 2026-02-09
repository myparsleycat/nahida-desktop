import { protocol } from "electron";
import LocalProtocol from "@main/services/protocol/local";

export function registerLocalProtocal() {
    const localProtocol = new LocalProtocol();
    protocol.handle("local", localProtocol.handle);
}
