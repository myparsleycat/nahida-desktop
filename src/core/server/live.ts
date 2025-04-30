import { BrowserWindow } from "electron";
import { Elysia, t } from "elysia";

const live = (app: Elysia) =>
  app
    .get('/download', async () => {
      const win = new BrowserWindow({ width: 800, height: 600 })
      win.loadURL('https://github.com');
    })

export default live;