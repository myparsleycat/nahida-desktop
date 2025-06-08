// src/core/server/live.ts

import { Elysia, t } from "elysia";
import { mainWindow } from "@main/window";
import { RendererCallManager } from "@core/ipc";

const live = (app: Elysia) =>
  app
    .post('/download', async ({ body }) => {
      const { url, name } = body;

      mainWindow.show();
      mainWindow.moveTop();
      mainWindow.focus();

      const selectedCharPath = await RendererCallManager.getSelectedCharPath();
      console.log('Selected character path:', selectedCharPath);

    }, {
      body: t.Object({
        url: t.String(),
        name: t.String()
      })
    })

export default live;