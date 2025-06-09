import { writable, get, derived } from 'svelte/store';
import type { FileInfo, ModFolders, ReadDirectoryOptions } from '@shared/types/fs.types';
import type { DirectChildren, Games, getDirectChildrenOptions } from '@shared/types/mods.types';
import type { IniParseResult } from '@core/lib/InIUtil';

class ModsHelperClass {
  resizableSize = writable(20);
  currentFolderPath = writable("");
  folders = writable<ModFolders[]>();
  folderChildren = writable<DirectChildren[]>([]);
  currentCharPath = writable("");

  clearPath() {
    this.currentFolderPath.set("");
    this.currentCharPath.set("");
    window.api.mods.clearPath();
  }

  folder = {
    getAll: async () => await window.api.mods.folder.getAll(),
    create: async (name: string, path: string) => await window.api.mods.folder.create(name, path),
    delete: async (path: string) => await window.api.mods.folder.delete(path),
    changeSeq: async (path: string, newSeq: number) => await window.api.mods.folder.changeSeq(path, newSeq),
    dir: {
      read: async (path: string, options?: ReadDirectoryOptions) => await window.api.mods.folder.dir.read(path, options),
      disableAll: async (path: string) => await window.api.mods.folder.dir.disableAll(path),
      enableAll: async (path: string) => await window.api.mods.folder.dir.enableAll(path)
    },
    read: async (path: string) => await window.api.mods.folder.read(path)
  }

  ui = {
    resizable: {
      get: async () => await window.api.mods.ui.resizable.get(),
      set: async (size: number) => {
        this.resizableSize.set(size);
        await window.api.mods.ui.resizable.set(size);
      }
    },
    layout: {
      get: async () => await window.api.mods.ui.layout.get(),
      set: async (layout: 'grid' | 'list') => await window.api.mods.ui.layout.set(layout)
    }
  }

  mod = {
    read: async (path: string) => await window.api.mods.mod.read(path),
    toggle: async (path: string) => await window.api.mods.mod.toggle(path),
    fix: async (path: string, game: Games) => await window.api.mods.mod.fix(path, game)
  }

  ini = {
    parse: async (path: string) => await window.api.mods.ini.parse(path),
    update: async (path: string, section: string, key: 'key' | 'back', value: string) => await window.api.mods.ini.update(path, section, key, value)
  }

  intx = {
    drop: async (data: string[]) => await window.api.mods.intx.drop(data)
  }
}

const ModsHelper = new ModsHelperClass();
export { ModsHelper };