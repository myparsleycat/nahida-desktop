import LocalProtocol from "@main/services/protocol/local";
import { protocol } from "electron";

export function registerLocalProtocal() {
    const localProtocol = new LocalProtocol();
    protocol.handle("local", localProtocol.handle);
}
