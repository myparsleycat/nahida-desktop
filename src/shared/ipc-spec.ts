export const IPC_EVENT_CHANNELS = [
    "window:blur",
    "window:focus",

    "transfer:update",

    "fn:toast",
    "fn:navi",
    "download:completed",
    "pathSelector:modeSelect",

    "mod:update-game",
    "mod:update-mods",
] as const;

export const IPC_HANDLER_CHANNELS = [
    "ping",

    "setting:general:getRunOnStartup",
    "setting:general:setRunOnStartup",
    "setting:general:getMoveTransferPageWhenStartTransfer",
    "setting:general:setMoveTransferPageWhenStartTransfer",
    "setting:general:getPowerSaveBlockInTransfer",
    "setting:general:setPowerSaveBlockInTransfer",
    "setting:general:getDefaultStartPage",
    "setting:general:setDefaultStartPage",
    "setting:general:checkUpdate",
    "setting:general:getCheckBackgroundUpdates",
    "setting:general:setCheckBackgroundUpdates",

    "setting:mod:getDeleteArchiveAfterExtract",
    "setting:mod:setDeleteArchiveAfterExtract",
    "setting:mod:getMoveFolderInsteadOfCopy",
    "setting:mod:setMoveFolderInsteadOfCopy",

    "window:closeWindow",
    "window:openReport",
    "window:openSetting",

    "auth:isLoggedIn",
    "auth:startLogin",
    "auth:startLogout",
    "auth:getSession",

    "util:showModal",
    "util:openExternal",
    "util:copyStr",
    "util:get:language",
    "util:openPath",
    "util:fs:trash",
    "util:openCmd",
    "util:getClipboardFiles",
    "util:fs:metadata",

    "drive:get:item",
    "drive:patch:rename",
    "drive:post:dir",
    "drive:delete:items",
    "drive:fn:startDownload",
    "drive:fn:startUpload",

    "transfer:list",
    "transfer:cancel",
    "transfer:pause",
    "transfer:resume",
    "transfer:retry",
    "transfer:pause-all",
    "transfer:resume-all",
    "transfer:clear",

    "mod:selectFolder",
    "mod:getGamePath",
    "mod:getGames",
    "mod:addGame",
    "mod:removeGame",
    "mod:pickFolder",

    "mod:getCharacters",
    "mod:getMods",
    "mod:toggle",
    "mod:updateToggleKey",
    "mod:getPresets",
    "mod:createPreset",
    "mod:applyPreset",
    "mod:deletePreset",
    "mod:updatePresetName",
    "mod:getLastGame",
    "mod:setLastGame",
    "mod:extractArchive",
    "mod:copyFolder",
    "mod:enableAll",
    "mod:disableAll",
    "mod:pastePreview",
    "mod:watchGame",
    "mod:watchCharacter",

    "pathSelector:selectFolderPath",
    "pathSelector:selectModManagerPath",
    "pathSelector:cancel",

    "logger:log",
] as const;

export type IpcEventChannel = (typeof IPC_EVENT_CHANNELS)[number];
export type IpcHandlerChannel = (typeof IPC_HANDLER_CHANNELS)[number];
