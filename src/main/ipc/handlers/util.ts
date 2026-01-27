import type { MessageBoxOptions, OpenExternalOptions } from "electron";
import { rh } from "../helper";
import {
    openExternal,
    showModal,
    copyStr,
    openPath,
    trash,
    openCmd,
    getClipboardFiles,
    getPathMetadata,
} from "@main/services/util";
import type { NahidaDesktop } from "@main/index";

export function registerUtilHandlers(_desktop: NahidaDesktop) {
    rh("util:showModal", async (options: MessageBoxOptions) => await showModal(options));
    rh(
        "util:openExternal",
        async (url: string, opt?: OpenExternalOptions) => await openExternal(url, opt),
    );
    rh("util:copyStr", copyStr);
    rh("util:openPath", openPath);
    rh("util:fs:trash", trash);
    rh("util:openCmd", openCmd);
    rh("util:getClipboardFiles", getClipboardFiles);
    rh("util:fs:metadata", getPathMetadata);
}
