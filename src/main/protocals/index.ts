import type { NahidaDesktop } from "@main/index";
import { registerLocalProtocal } from "./local";
import { registerModelViewerMemoryProtocol } from "./model-viewer-memory";

export function registerProtocal(desktop: NahidaDesktop) {
    registerLocalProtocal(desktop);
    registerModelViewerMemoryProtocol();
}
