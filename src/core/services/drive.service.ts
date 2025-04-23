// src/core/services/drive.service.ts

import { GetContentsUrl, RenameUrl } from "../const";
import { fetcher } from "../lib/fetcher";
import { GetContentsResp, RenameResp } from "../../types/drive.types";
import { imageCache } from "../lib/imageCache";
import { fss } from "./fs.service";

class DriveService {
  item = {
    get: async (id: string) => {
      const url = GetContentsUrl + id;
      const resp = await fetcher<GetContentsResp>(url)
      if (!resp.data.success) {
        throw new Error(`item.get error: ${resp.data.error?.message}`);
      }

      return resp.data;
    },

    rename: async (id: string, rename: string) => {
      const url = RenameUrl + id;
      const resp = await fetcher<RenameResp>(url, {
        method: 'POST',
        body: { rename }
      });

      if (!resp.data.success) {
        throw new Error(`item.rename error: ${resp.data.error?.message}`);
      }
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