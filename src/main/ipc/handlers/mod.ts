import path from "path";
import { dialog } from "electron";
import { rh } from "@main/ipc/helper";
import { NahidaDesktop } from "@main/index";

export function registerModHandlers(desktop: NahidaDesktop) {
    rh("mod:selectFolder", async (game: string) => {
        const result = await dialog.showOpenDialog({
            properties: ["openDirectory"],
            title: `Select ${game} Mod Folder`,
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        const folderPath = result.filePaths[0];
        await desktop.service.mod.fn.setGamePath(game, folderPath);
        return folderPath;
    });

    rh("mod:getGamePath", async (game: string) => {
        return await desktop.service.mod.get.gamePath(game);
    });

    rh("mod:getGames", async () => {
        return await desktop.service.mod.get.games();
    });

    rh("mod:addGame", async (game: string, path: string) => {
        return await desktop.service.mod.fn.addGame(game, path);
    });

    rh("mod:removeGame", async (game: string) => {
        return await desktop.service.mod.fn.removeGame(game);
    });

    rh("mod:pickFolder", async () => {
        const result = await dialog.showOpenDialog({
            properties: ["openDirectory"],
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        return result.filePaths[0];
    });

    rh("mod:getCharacters", async (game: string, searchModPreview?: boolean) => {
        return await desktop.service.mod.get.characters(game, searchModPreview);
    });

    rh("mod:getMods", async (groupPath: string) => {
        return await desktop.service.mod.get.mods(groupPath);
    });

    rh("mod:toggle", async (modPath: string) => {
        return await desktop.service.mod.fn.toggle(modPath);
    });

    rh("mod:exclusiveToggle", async (modPath: string) => {
        return await desktop.service.mod.fn.exclusiveToggle(modPath);
    });

    rh("mod:enableAll", async (groupPath: string) => {
        return await desktop.service.mod.fn.enableAll(groupPath);
    });

    rh("mod:disableAll", async (groupPath: string) => {
        return await desktop.service.mod.fn.disableAll(groupPath);
    });

    rh(
        "mod:updateToggleKey",
        async (
            modPath: string,
            iniFileName: string,
            sectionName: string,
            variable: string,
            value: string,
        ) => {
            const iniPath = path.join(modPath, iniFileName);
            return await desktop.service.mod.fn.updateToggleKey(
                iniPath,
                sectionName,
                variable,
                value,
            );
        },
    );

    rh("mod:getPresets", async (game: string) => {
        return await desktop.service.mod.get.presets(game);
    });

    rh("mod:createPreset", async (game: string, name: string) => {
        return await desktop.service.mod.fn.createPreset(game, name);
    });

    rh("mod:applyPreset", async (presetId: string) => {
        return await desktop.service.mod.fn.applyPreset(presetId);
    });

    rh("mod:deletePreset", async (presetId: string) => {
        return await desktop.service.mod.fn.deletePreset(presetId);
    });

    rh("mod:updatePresetName", async (presetId: string, newName: string) => {
        return await desktop.service.mod.fn.updatePresetName(presetId, newName);
    });

    rh("mod:getLastGame", async () => {
        return await desktop.service.mod.get.lastGame();
    });

    rh("mod:getPreviousFocusedGame", async () => {
        return await desktop.service.mod.get.previousFocusedGame();
    });

    rh("mod:setLastGame", async (game: string) => {
        return await desktop.service.mod.fn.setLastGame(game);
    });

    rh("mod:extractArchive", async (archivePath: string, groupPath: string) => {
        return await desktop.service.mod.fn.extractArchiveToGroup(archivePath, groupPath);
    });

    rh("mod:copyFolder", async (folderPath: string, groupPath: string) => {
        const moveInsteadOfCopy = await desktop.setting.mod.getMoveFolderInsteadOfCopy();
        return await desktop.service.mod.fn.copyFolderToGroup(
            folderPath,
            groupPath,
            moveInsteadOfCopy,
        );
    });

    rh(
        "mod:pastePreview",
        async (modPath: string, data: string, type: "url" | "base64" | "path") => {
            return await desktop.service.mod.fn.pastePreview(modPath, data, type);
        },
    );

    rh("mod:watchGame", async (game: string) => {
        return await desktop.service.mod.watchGame(game);
    });

    rh("mod:watchCharacter", async (characterPath: string) => {
        return await desktop.service.mod.watchCharacter(characterPath);
    });
}
