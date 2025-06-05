import { BrowserWindow } from "electron";
import { Elysia, t } from "elysia";
import { mainWindow } from "../../main/window";

const live = (app: Elysia) =>
  app
    .post('/download', async ({ body }) => {
      const { url, name } = body;

      mainWindow.show();
      mainWindow.moveTop();
      mainWindow.focus();

      console.log(url, name);
    }, {
      body: t.Object({
        url: t.String(),
        name: t.String()
      })
    })

export default live;