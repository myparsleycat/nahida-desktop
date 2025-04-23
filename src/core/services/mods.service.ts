import { db } from "../db";
import { fss } from "./fs.service";
import type { FileInfo, ModFolders, ReadDirectoryOptions } from "../../types/fs.types";
import { nanoid } from 'nanoid';

class ModsService {
  ui = {
    resizable: {
      get: async () => {
        const size = await db.get("LocalStorage", "mods_resizable_default");
        return size || 20;
      },

      set: async (size: number) => {
        await db.update("LocalStorage", "mods_resizable_default", size);
      }
    }
  }

  folder = {
    getAll: async () => {
      return await db.query(`SELECT * from ModFolders`) as ModFolders[];
    },
    create: async (path: string, name: string) => {
      await db.insert("ModFolders", nanoid(), { path, name });
    },

    dir: {
      read: async (path: string, options?: ReadDirectoryOptions) => {
        return await fss.readDirectory(path, options) as FileInfo[];
      }
    }
  }

  async fix(path: string) {
    console.log(path);
  }
}

const mods = new ModsService();
export { mods };