// src/core/ipc/channels/mods.ts
import type { ModsService } from '@core/services';
import { ChannelGroup } from '../registry';
import type { ReadDirectoryOptions } from '@shared/types/fs.types';
import type { Fixx } from '@shared/types/mods.types';

export function defineModsChannels(rootGroup: ChannelGroup) {
    const modsGroup = rootGroup.addGroup('mods');
    modsGroup.addChannel('clearPath');

    const modsUiGroup = modsGroup.addGroup('ui');
    const modsUiResizableGroup = modsUiGroup.addGroup('resizable');
    const modsUiLayoutGroup = modsUiGroup.addGroup('layout');
    modsUiResizableGroup.addChannel('get');
    modsUiResizableGroup.addChannel('set');
    modsUiLayoutGroup.addChannel('get');
    modsUiLayoutGroup.addChannel('set');

    const modsFolderGroup = modsGroup.addGroup('folder');
    modsFolderGroup.addChannel('getAll');
    modsFolderGroup.addChannel('create');
    modsFolderGroup.addChannel('delete');
    modsFolderGroup.addChannel('changeSeq');
    modsFolderGroup.addChannel('read');

    const modsFolderDirGroup = modsFolderGroup.addGroup('dir');
    modsFolderDirGroup.addChannel('read');
    modsFolderDirGroup.addChannel('disableAll');
    modsFolderDirGroup.addChannel('enableAll');

    const modGroup = modsGroup.addGroup('mod');
    modGroup.addChannel('read');
    modGroup.addChannel('toggle');
    modGroup.addChannel('fix');

    const iniGroup = modsGroup.addGroup('ini');
    iniGroup.addChannel('parse');
    iniGroup.addChannel('update');

    const intxGroup = modsGroup.addGroup('intx');
    intxGroup.addChannel('drop');

    const modsMsgGroup = modsGroup.addGroup('msg');
    modsMsgGroup.addChannel('currentCharPathChanged', undefined, true, 'current-char-path-changed');
    modsMsgGroup.addChannel('currentFolderPathChanged', undefined, true, 'current-folder-path-changed');
}

export function injectModsHandlers(registry: any, mods: typeof ModsService) {
    registry.injectHandlers('mods', {
        clearPath: () => mods.clearPath()
    });

    registry.injectHandlers('mods.ui.resizable', {
        get: () => mods.ui.resizable.get(),
        set: (size: number) => mods.ui.resizable.set(size)
    });

    registry.injectHandlers('mods.ui.layout', {
        get: () => mods.ui.layout.get(),
        set: (layout: 'grid' | 'list') => mods.ui.layout.set(layout)
    });

    registry.injectHandlers('mods.folder', {
        getAll: () => mods.folder.getAll(),
        create: (name: string, path: string) => mods.folder.create(path, name),
        delete: (path: string) => mods.folder.delete(path),
        changeSeq: (path: string, newSeq: number) => mods.folder.changeSeq(path, newSeq),
        read: (path: string) => mods.folder.read(path)
    });

    registry.injectHandlers('mods.folder.dir', {
        read: (path: string, options?: ReadDirectoryOptions) => mods.folder.dir.read(path, options),
        disableAll: (path: string) => mods.folder.dir.disableAll(path),
        enableAll: (path: string) => mods.folder.dir.enableAll(path)
    });

    registry.injectHandlers('mods.mod', {
        read: (path: string) => mods.mod.read(path),
        toggle: (path: string) => mods.mod.toggle(path),
        fix: (path: string, fix: Fixx) => mods.mod.fix(path, fix)
    });

    registry.injectHandlers('mods.ini', {
        parse: (path: string) => mods.ini.parse(path),
        update: (path: string, section: string, key: 'key', value: string) => mods.ini.update(path, section, key, value)
    });

    registry.injectHandlers('mods.intx', {
        drop: (data: string[]) => mods.intx.drop(data)
    });
}