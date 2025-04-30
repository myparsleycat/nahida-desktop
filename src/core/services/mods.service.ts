import { db } from "../db";
import { fss } from "./fs.service";
import type { FileInfo, ModFolders, ReadDirectoryOptions } from "../../types/fs.types";
import { nanoid } from 'nanoid';
import { basename, dirname, join } from "node:path";
import { iniutil as ini } from "../lib/InIUtil";
import fs from 'node:fs';

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
    create: async (path: string, name: string) => await db.insert("ModFolders", nanoid(), { path, name }),
    delete: async (path: string) => {
      return await db.exec(`DELETE FROM ModFolders WHERE path = '${path}'`);
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

  ini = {
    parse: async (path: string) => {
      try {
        const content = await fss.readFile(path, "utf8");
        return ini.parse(content);
      } catch (err: any) {
        console.error(`Error parsing INI file: ${err.message}`);
        return null;
      }
    },

    update: async (path: string, section: string, key: 'key', value: string) => {
      try {
        const content = await fss.readFile(path, "utf8");
        const updatedContent = ini.update(content, section, key, value);
        await fs.promises.writeFile(path, updatedContent, { encoding: 'utf8' });
        return true;
      } catch (err: any) {
        console.error(`Error updating INI file: ${err.message}`);
        throw err;
      }
    }
  }

  async fix(path: string) {
    console.log(path);
  }
}

const mods = new ModsService();
export { mods };