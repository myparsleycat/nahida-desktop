import type { NahidaDesktop } from "@main/index";
import { registerLocalProtocal } from "./local";
import { registerModelViewerMemoryProtocal } from "./model-viewer-memory";

export function registerProtocal(desktop: NahidaDesktop) {
    registerLocalProtocal(desktop);
    registerModelViewerMemoryProtocal();
}
