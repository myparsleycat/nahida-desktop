// src/core/server/live.ts

import { Elysia, t } from "elysia";
import { mainWindow } from "@main/window";
import { RendererCallManager } from "@core/ipc";
import { NahidaService, ToastService } from "@core/services";
import { NahidaTransferService } from "@core/services/nahida.transfer.service";

const live = (app: Elysia) =>
  app
    .post('/download', async ({ body }) => {
      const { url, name } = body;

      mainWindow.show();
      mainWindow.moveTop();
      mainWindow.focus();

      RendererCallManager.getSelectedCharPath()
        .then(path => {
          NahidaTransferService.download.enqueue(url, path, {
            name
          })
            .then(() => {
              ToastService.success(`${name} 다운로드 시작`);
            })
        })
        .catch((err: any) => {
          if (err.message === 'Timeout') {
            RendererCallManager.closeCharPathSelector();
          }
        });

      return { success: true };
    }, {
      body: t.Object({
        url: t.String(),
        name: t.String()
      })
    })

export default live;