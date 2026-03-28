import type { NahidaDesktop } from "@main/index";
import {
    copyStr,
    getAppStatus,
    getClipboardFiles,
    getPathMetadata,
    mkdir,
    openCmd,
    openExternal,
    openPath,
    openReportWindow,
    showModal,
    showOpenDialog,
    submitReport,
    trash,
} from "@main/services/util";
import type { MessageBoxOptions, OpenExternalOptions } from "electron";
import { rh } from "../helper";

export function registerUtilHandlers(desktop: NahidaDesktop) {
    rh("util:getAppStatus", getAppStatus);
    rh("util:showModal", async (options: MessageBoxOptions) => await showModal(options));
    rh(
        "util:openExternal",
        async (url: string, opt?: OpenExternalOptions) => await openExternal(url, opt),
    );
    rh("util:copyStr", copyStr);
    rh("util:openPath", openPath);
    rh("util:fs:mkdir", mkdir);
    rh("util:fs:trash", trash);
    rh("util:openCmd", openCmd);
    rh("util:getClipboardFiles", getClipboardFiles);
    rh("util:fs:metadata", getPathMetadata);
    rh("util:showOpenDialog", showOpenDialog);
    rh("util:openReportWindow", openReportWindow);
    rh("util:submitReport", submitReport);
    rh("util:fs:rename", async (oldPath, newPath) => await desktop.lib.fs.rename(oldPath, newPath));
}
