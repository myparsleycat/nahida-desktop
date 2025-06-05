// src/core/services/drive.service.ts

import {
  DirCreateManyUrl,
  GetContentsUrl,
  MoveManyUrl,
  RenameUrl,
  TrashManyUrl
} from "../const";
import {
  DirCreateManyResp,
  GetContentsResp,
  MoveManyResp,
  RenameResp,
  TrashManyResp
} from "@shared/types/drive.types";
import { api, fetcher } from "@core/lib/fetcher";
import { imageCache } from "@core/lib/imageCache";
import { fss } from "./fs.service";
import { TFS } from "./akasha.transfer.service";

class AkashaDriveService {
  item = {
    get: async (id: string) => {
      const url = GetContentsUrl + id;
      const resp = await fetcher<GetContentsResp>(url)
      if (!resp.data.success) {
        throw new Error(`item.get error: ${resp.data.error.message}`);
      }
      return resp.data;
    },

    move: async (current: string, ids: string[], newParentId: string) => {
      const resp = await fetcher<MoveManyResp>(MoveManyUrl, {
        method: 'POST',
        body: {
          current,
          uuids: ids,
          target: newParentId
        }
      });
      if (!resp.data.success) {
        throw new Error(resp.data.error.message);
      }
      return resp.data;
    },

    dir: {
      create: async (parentId: string, dirs: { name: string; path: string; }[]) => {
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
    },

    rename: async (id: string, rename: string) => {
      const url = RenameUrl + id;
      const resp = await api.post<RenameResp>(url, {
        rename
      })
      if (!resp.data.success) {
        throw new Error(resp.data.error.message);
      }
      return resp.data;
    },

    download: async (id: string) => {
      const save_path = await fss.select({ properties: ['openDirectory'] });
      if (!save_path) return;

      await TFS.akasha.download.enqueue(id, save_path);
    },

    async trash_many(ids: string[]) {
      const resp = await api.post<TrashManyResp>(TrashManyUrl, {
        uuids: ids
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

const ADS = new AkashaDriveService;
export { ADS };