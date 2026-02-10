import type { NahidaDesktop } from "@main/index";
import {
    copyStr,
    getAppStatus,
    getClipboardFiles,
    getPathMetadata,
    openCmd,
    openExternal,
    openPath,
    showModal,
    showOpenDialog,
    trash,
} from "@main/services/util";
import type { MessageBoxOptions, OpenExternalOptions } from "electron";
import { rh } from "../helper";

export function registerUtilHandlers(_desktop: NahidaDesktop) {
    rh("util:getAppStatus", getAppStatus);
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
    rh("util:showOpenDialog", showOpenDialog);
}
