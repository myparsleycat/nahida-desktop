import { db } from "../db";
import { fss } from "./fs.service";
import type { FileInfo, ModFolders, ReadDirectoryOptions } from "../../types/fs.types";
import { nanoid } from 'nanoid';
import { basename, dirname, join } from "node:path";

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
    },
  }

  mod = {
    toggle: async (path: string) => {
      const dirName = basename(path);
      const parentDir = dirname(path);

      if (dirName.toLowerCase().startsWith("disabled")) {
        const newName = dirName.replace(/^disabled\s+/i, "");
        await fss.rename(path, join(parentDir, newName));
        return true;
      } else {
        const newName = "DISABLED " + dirName;
        await fss.rename(path, join(parentDir, newName));
        return true;
      }
    }
  }

  async fix(path: string) {
    console.log(path);
  }
}

const mods = new ModsService();
export { mods };