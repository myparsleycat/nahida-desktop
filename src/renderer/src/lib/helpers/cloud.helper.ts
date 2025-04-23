import { writable, get, derived } from 'svelte/store';
import { _ } from 'svelte-i18n';

class CloudHelper {
  currentId = writable("root");

  nav = {
    get: () => get(this.currentId),
    move: (id: string) => this.currentId.set(id)
  }

  item = {
    get: async (id: string) => await window.api.drive.item.get(id),
    rename: async (id: string, name: string) => await window.api.drive.item.rename(id, name),
    download: {
      enqueue: async (id: string) => window.api.drive.item.download.enqueue(id),
    },
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

const Cloud = new CloudHelper;
export { Cloud };