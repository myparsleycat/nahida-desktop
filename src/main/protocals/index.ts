import type { NahidaDesktop } from "@main/index";
import { registerLocalProtocal } from "./local";

export function registerProtocal(desktop: NahidaDesktop) {
    registerLocalProtocal();
}
