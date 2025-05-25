import { writable, get, derived } from 'svelte/store';
import { _ } from 'svelte-i18n';

class NahidaDriveHelper {
  currentId = writable("root");

  nav = {
    get: () => get(this.currentId),
    move: (id: string) => this.currentId.set(id)
  }

  item = {
    get: async (id: string) => await window.api.drive.item.get(id),
    move: async (current: string, ids: string[], newParentId: string) => await window.api.drive.item.move(current, ids, newParentId),
    dir: {
      create: async (parentId: string, dirs: { name: string; path: string; }[]) => await window.api.drive.item.dir.create(parentId, dirs),
    },
    rename: async (id: string, name: string) => await window.api.drive.item.rename(id, name),
    download: {
      enqueue: async (id: string, name: string) => window.api.drive.item.download.enqueue(id, name),
    },
    trash_many: async (ids: string[]) => await window.api.drive.item.trash_many(ids)
  }

  util = {
    imageCache: {
      get: async (url: string) => await window.api.drive.util.imageCache.get(url),
      sizes: async () => await window.api.drive.util.imageCache.sizes(),
      clear: async () => await window.api.drive.util.imageCache.clear(),
      getStates: async () => await window.api.drive.util.imageCache.getStates(),
      change: async (v: boolean) => await window.api.drive.util.imageCache.change(v),
    }
  }
}

const NDH = new NahidaDriveHelper;
export { NDH };