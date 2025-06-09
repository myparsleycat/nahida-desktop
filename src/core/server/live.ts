// src/core/server/live.ts

import { Elysia, t } from "elysia";
import { focus } from "@main/window";
import { RendererCallManager } from "@core/ipc";
import { ToastService } from "@core/services";
import { NahidaTransferService } from "@core/services/nahida.transfer.service";
import { modDTO } from "@core/server/types";

const live = (app: Elysia) =>
    app
        .post('/download', async ({ body }) => {
            const { url, mod } = body;

            focus();

            RendererCallManager.getSelectedCharPath()
                .then(path => {
                    NahidaTransferService.download.enqueue(url, path, mod)
                        .then(() => {
                            ToastService.success(`${mod.title} 다운로드를 시작합니다`);
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
                mod: modDTO,
            })
        })

export default live;