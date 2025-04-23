import { fileTypeFromBuffer } from "file-type";
import { db } from "../db";

class ImageCache {
  async getCacheState() {
    const state = await db.get("LocalStorage", "img_cache_on")
    return !!state;
  }

  async fetchImage(url: string) {
    return Buffer.from(await (await fetch(url)).arrayBuffer());
  }

  async get(url: string) {
    const state = this.getCacheState();

    if (!state) {
      const buf = await this.fetchImage(url);
      const filetype = await fileTypeFromBuffer(buf);
      return `data:${filetype?.mime};base64,${buf.toString('base64')}`
    } else {
      const pathname = new URL(url).pathname;
      const match = pathname.match(
        /([a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})/
      );
      if (!match) {
        throw new Error(`Invalid URL format, no UUID found: ${url}`);
      }
      const key = match[1];

      const table = db.table("ImageCache");

      const cachedImg = await table.get(key);
      if (cachedImg) {
        return `data:${cachedImg.mimeType};base64,${cachedImg.image.toString('base64')}`
      } else {
        const buf = await this.fetchImage(url);
        const filetype = await fileTypeFromBuffer(buf);
        await table.insert(key, buf);
        return `data:${filetype?.mime};base64,${buf.toString('base64')}`
      }
    }
  }

  async sizes() {
    const sql = `SELECT SUM(size) as size FROM ImageCache;`
    const result = await db.query(sql) as any;
    return result[0].size as number;
  }

  async clear() {
    const sql = `DELETE FROM ImageCache;`
    return await db.exec(sql);
  }

  async getStates() {
    const state = await this.getCacheState();
    const sizes = await this.sizes();
    return { state, sizes };
  }

  async change(v: boolean) {
    await db.update("LocalStorage", "img_cache_on", v)
  }
}

const imageCache = new ImageCache();
export { imageCache };