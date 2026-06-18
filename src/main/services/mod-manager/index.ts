import type { ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { NahidaDesktop } from "../..";
import { ModF10Service } from "./f10";
import { ModImportsService } from "./imports";
import { ModIniService } from "./ini";
import { ModLibraryService } from "./library";
import { ModActionsService } from "./mod-actions";
import { ModPresetsService } from "./presets";
import { ModShaderFixesService } from "./shader-fixes";
import { ModWatchersService } from "./watchers";

export class ModManager {
    private readonly library: ModLibraryService;
    private readonly shaderFixes: ModShaderFixesService;
    private readonly actions: ModActionsService;
    private readonly presets: ModPresetsService;
    private readonly imports: ModImportsService;
    private readonly ini: ModIniService;
    private readonly watchers: ModWatchersService;
    private readonly f10: ModF10Service;

    public readonly get: {
        gamePath: ModLibraryService["gamePath"];
        characters: ModLibraryService["characters"];
        subGroups: ModLibraryService["subGroups"];
        manualSubGroups: ModLibraryService["manualSubGroups"];
        mods: ModLibraryService["mods"];
        presets: ModPresetsService["presets"];
        presetCreateConflicts: ModPresetsService["presetCreateConflicts"];
        games: ModLibraryService["games"];
        lastGame: ModLibraryService["lastGame"];
        expandedGroups: ModLibraryService["expandedGroups"];
        previousFocusedGame: ModLibraryService["previousFocusedGame"];
        gamePid: ModLibraryService["gamePid"];
        resolveDownloadTarget: ModLibraryService["resolveDownloadTarget"];
    };

    public readonly fn: {
        setGamePath: ModLibraryService["setGamePath"];
        enable: ModActionsService["enable"];
        disable: ModActionsService["disable"];
        toggle: ModActionsService["toggle"];
        exclusiveToggle: ModActionsService["exclusiveToggle"];
        rename: ModActionsService["rename"];
        enableAll: ModActionsService["enableAll"];
        disableAll: ModActionsService["disableAll"];
        updateToggleKey: ModIniService["updateToggleKey"];
        createPreset: ModPresetsService["createPreset"];
        applyPreset: ModPresetsService["applyPreset"];
        deletePreset: ModPresetsService["deletePreset"];
        updatePresetName: ModPresetsService["updatePresetName"];
        addGame: ModLibraryService["addGame"];
        updateGame: ModLibraryService["updateGame"];
        reorderGames: ModLibraryService["reorderGames"];
        removeGame: ModLibraryService["removeGame"];
        setLastGame: ModLibraryService["setLastGame"];
        setExpandedGroups: ModLibraryService["setExpandedGroups"];
        setManualSubGroup: ModLibraryService["setManualSubGroup"];
        extractArchiveToGroup: (
            archivePath: string,
            groupPath: string,
            mode?: ResolvedArchiveExtractPathMode,
        ) => Promise<void>;
        copyFolderToGroup: ModImportsService["copyFolderToGroup"];
        pastePreview: ModImportsService["pastePreview"];
        triggerF10: ModF10Service["triggerF10"];
    };

    constructor(desktop: NahidaDesktop) {
        this.library = new ModLibraryService(desktop);
        this.shaderFixes = new ModShaderFixesService(desktop, this.library);
        this.actions = new ModActionsService(desktop, this.shaderFixes);
        this.presets = new ModPresetsService(desktop, this.library, this.actions);
        this.imports = new ModImportsService(desktop, this.shaderFixes);
        this.ini = new ModIniService(desktop);
        this.watchers = new ModWatchersService(desktop, this.library);
        this.f10 = new ModF10Service(desktop, this.library);

        this.get = {
            gamePath: this.library.gamePath.bind(this.library),
            characters: this.library.characters.bind(this.library),
            subGroups: this.library.subGroups.bind(this.library),
            manualSubGroups: this.library.manualSubGroups.bind(this.library),
            mods: this.library.mods.bind(this.library),
            presets: this.presets.presets.bind(this.presets),
            presetCreateConflicts: this.presets.presetCreateConflicts.bind(this.presets),
            games: this.library.games.bind(this.library),
            lastGame: this.library.lastGame.bind(this.library),
            expandedGroups: this.library.expandedGroups.bind(this.library),
            previousFocusedGame: this.library.previousFocusedGame.bind(this.library),
            gamePid: this.library.gamePid.bind(this.library),
            resolveDownloadTarget: this.library.resolveDownloadTarget.bind(this.library),
        };

        this.fn = {
            setGamePath: this.library.setGamePath.bind(this.library),
            enable: this.actions.enable.bind(this.actions),
            disable: this.actions.disable.bind(this.actions),
            toggle: this.actions.toggle.bind(this.actions),
            exclusiveToggle: this.actions.exclusiveToggle.bind(this.actions),
            rename: this.actions.rename.bind(this.actions),
            enableAll: this.actions.enableAll.bind(this.actions),
            disableAll: this.actions.disableAll.bind(this.actions),
            updateToggleKey: this.ini.updateToggleKey.bind(this.ini),
            createPreset: this.presets.createPreset.bind(this.presets),
            applyPreset: this.presets.applyPreset.bind(this.presets),
            deletePreset: this.presets.deletePreset.bind(this.presets),
            updatePresetName: this.presets.updatePresetName.bind(this.presets),
            addGame: this.library.addGame.bind(this.library),
            updateGame: this.library.updateGame.bind(this.library),
            reorderGames: this.library.reorderGames.bind(this.library),
            removeGame: this.library.removeGame.bind(this.library),
            setLastGame: this.library.setLastGame.bind(this.library),
            setExpandedGroups: this.library.setExpandedGroups.bind(this.library),
            setManualSubGroup: this.library.setManualSubGroup.bind(this.library),
            extractArchiveToGroup: this.imports.extractArchiveToGroup.bind(this.imports),
            copyFolderToGroup: this.imports.copyFolderToGroup.bind(this.imports),
            pastePreview: this.imports.pastePreview.bind(this.imports),
            triggerF10: this.f10.triggerF10.bind(this.f10),
        };
    }

    public async watchGame(game: string) {
        await this.watchers.watchGame(game);
    }

    public async watchCharacter(characterPath: string) {
        await this.watchers.watchCharacter(characterPath);
    }
}

export default ModManager;
