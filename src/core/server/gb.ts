import { Elysia, t } from 'elysia';
import { focus } from "@main/window";
import { RendererCallManager } from "@core/ipc";
import { GBTransferService, ToastService } from "@core/services";
import { GameBananaClass } from '@core/lib/gamebanana';

const gb = (app: Elysia) => app
    .post('/download', async ({ body }) => {
        const { modId, fileInfoId, url } = body;

        const mod = new GameBananaClass(modId);
        const modData = await mod.GetModProfile();
        if (!modData) {
            ToastService.error("모드 정보를 불러오지 못했습니다.");
            return false;
        }

        const fileData = mod.GetFileData(fileInfoId);

        focus();

        RendererCallManager.getSelectedCharPath()
            .then(path => {
                GBTransferService.download.enqueue(path, modData, fileData, url)
                    .then(() => {
                        ToastService.success(`${modData._sName} 다운로드를 시작합니다`);
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
            modId: t.Integer(),
            fileInfoId: t.Integer(),
            url: t.String()
        })
    })

export default gb;