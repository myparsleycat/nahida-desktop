import { writable, get, derived } from 'svelte/store';
import type { FileInfo, ModFolders, ReadDirectoryOptions } from '../../../../types/fs.types';
import type { DirectChildren, getDirectChildrenOptions } from '../../../../types/mods.types';
import type { IniParseResult } from '../../../../core/lib/InIUtil';

class ModsHelper {
  resizableSize = writable(20);
  currentFolderPath = writable("");
  folders = writable<ModFolders[]>();
  folderChildren = writable<DirectChildren[]>([]);
  currentCharPath = writable("");

  folder = {
    getAll: async () => await window.api.mods.folder.getAll(),
    create: async (name: string, path: string) => await window.api.mods.folder.create(name, path),
    delete: async (path: string) => await window.api.mods.folder.delete(path),
    dir: {
      read: async (path: string, options?: ReadDirectoryOptions) => await window.api.mods.folder.dir.read(path, options)
    }
  }

  ui = {
    resizable: {
      get: async () => await window.api.mods.ui.resizable.get(),
      set: async (size: number) => {
        this.resizableSize.set(size);
        await window.api.mods.ui.resizable.set(size);
      }
    }
  }

  mod = {
    toggle: async (path: string) => await window.api.mods.mod.toggle(path)
  }

  ini = {
    parse: async (path: string) => await window.api.mods.ini.parse(path),
    update: async (path: string, section: string, key: 'key', value: string) => await window.api.mods.ini.update(path, section, key, value)
  }

  async getDirectChildren(path: string, options: getDirectChildrenOptions = {}): Promise<DirectChildren[]> {
    const { recursive = 1, dirOnly = true } = options;

    const children = await window.api.fss.readDir(path, { recursive });

    const filteredChildren = dirOnly
      ? children.filter(child => child.isDirectory)
      : children;

    const data = filteredChildren.map(async (child) => {
      let iniData: IniParseResult[] | null = null;

      // 현재 폴더 내의 INI 파일 찾기
      const iniFiles = child.children?.filter(item =>
        !item.isDirectory &&
        item.name.endsWith('.ini') &&
        !item.name.toLowerCase().startsWith('disabled')
      ) || [];

      // 하위 폴더 내의 INI 파일도 찾기
      const subFolderIniFiles: FileInfo[] = [];
      if (child.children) {
        child.children.forEach(subChild => {
          if (subChild.isDirectory && subChild.children) {
            const subIniFiles = subChild.children.filter(item =>
              !item.isDirectory &&
              item.name.endsWith('.ini') &&
              !item.name.toLowerCase().startsWith('disabled')
            );
            subFolderIniFiles.push(...subIniFiles);
          }
        });
      }

      // 모든 INI 파일 합치기 (현재 폴더 먼저, 그 다음 하위 폴더)
      const allIniFiles = [...iniFiles, ...subFolderIniFiles];

      if (allIniFiles.length > 0) {
        try {
          iniData = await this.ini.parse(allIniFiles[0].path);
        } catch (error) {
          console.error(`Error parsing INI file at ${allIniFiles[0].path}:`, error);
        }
      }

      let previewPath: string | null = null;
      const previewImage = child.children?.find(item => item.name === 'preview');
      const firstImage = child.children?.find(item => !item.isDirectory &&
        ['jpg', 'jpeg', 'png', 'gif', 'avif', 'avifs'].includes(item.name.split('.').pop()!.toLowerCase()));

      if (previewImage) {
        previewPath = previewImage.path;
      } else if (firstImage) {
        previewPath = firstImage.path;
      }

      let preview: { path: string, base64: string | null } | null = null;
      if (previewPath) {
        preview = { path: previewPath, base64: null };
      }

      return {
        path: child.path,
        name: child.name,
        ini: iniData ? {
          path: allIniFiles[0].path,
          data: iniData
        } : null,
        preview
      };
    });

    return Promise.all(data);
  }
}

const Mods = new ModsHelper();
export { Mods };