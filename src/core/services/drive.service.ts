// src/core/services/drive.service.ts

import { DirCreateManyUrl, GetContentsUrl, RenameUrl, TrashManyUrl } from "../const";
import { fetcher } from "../lib/fetcher";
import { DirCreateManyResp, GetContentsResp, RenameResp, TrashManyResp } from "../../types/drive.types";
import { imageCache } from "../lib/imageCache";
import { fss } from "./fs.service";

class DriveService {
  item = {
    get: async (id: string) => {
      const url = GetContentsUrl + id;
      const resp = await fetcher<GetContentsResp>(url)
      if (!resp.data.success) {
        throw new Error(`item.get error: ${resp.data.error.message}`);
      }
      return resp.data;
    },

    create_dirs: async (parentId: string, dirs: { name: string; path: string; }[]) => {
      const resp = await fetcher<DirCreateManyResp>(DirCreateManyUrl, {
        method: 'POST',
        body: {
          current: parentId,
          parentId,
          dirs
        }
      });
      if (!resp.data.success) {
        throw new Error(`item.create_dir error: ${resp.data.error.message}`);
      }
      return resp.data
    },

    rename: async (id: string, rename: string) => {
      const url = RenameUrl + id;
      const resp = await fetcher<RenameResp>(url, {
        method: 'POST',
        body: { rename }
      });
      if (!resp.data.success) {
        throw new Error(resp.data.error.message);
      }
      return resp.data;
    },

    download: {
      enqueue: async (id: string) => {
        const [save_path] = await Promise.all([
          fss.select({ properties: ['openDirectory'] })
        ])

        if (!save_path) return;

        console.log(id);

        return;
      }
    },

    async trash_many(ids: string[]) {
      const resp = await fetcher<TrashManyResp>(TrashManyUrl, {
        method: 'POST',
        body: { uuids: ids }
      });
      if (!resp.data.success) {
        throw new Error(`item.trash_many error: ${resp.data.error.message}`);
      }
      return resp.data;
    }
  }

  util = {
    imageCache: {
      get: async (url: string) => imageCache.get(url),
      sizes: async () => imageCache.sizes(),
      clear: async () => imageCache.clear(),
      getStates: async () => imageCache.getStates(),
      change: async (v: boolean) => imageCache.change(v)
    }
  }
}

const drive = new DriveService;
export { drive };