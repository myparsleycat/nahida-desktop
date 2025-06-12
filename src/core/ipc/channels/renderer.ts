// src/core/ipc/channels/renderer.ts
import { nanoid } from 'nanoid';
import { ChannelGroup } from '../registry';

const HOUR = 60000 * 1 * 60;

export function defineRendererChannels(rootGroup: ChannelGroup) {
    const rendererGroup = rootGroup.addGroup('renderer');

    rendererGroup.addChannel('requestCharPath', undefined, true, 'renderer:request-char-path');
    rendererGroup.addChannel('charPathResponse');

    rendererGroup.addChannel('closeCharPathSelector', undefined, true, 'renderer:close-char-path-selector');
}

let pendingRequests = new Map<string, any>();

export function injectRendererHandlers(registry: any) {
    registry.injectHandlers('renderer', {
        charPathResponse: (requestId: string, result: { success: boolean; data: string }) => {
            const pending = pendingRequests.get(requestId);
            if (pending) {
                clearTimeout(pending.timeout);
                pendingRequests.delete(requestId);

                if (result.success) {
                    pending.resolve(result.data);
                } else {
                    pending.reject(new Error('선택되지 않거나 에러 발생'));
                }
            } else {
                console.log('No pending request found for:', requestId);
            }
        }
    });
}

export const RendererCallManager = {
    async getSelectedCharPath(): Promise<string> {
        const { mainWindow } = await import('@main/window');

        return new Promise((resolve, reject) => {
            const requestId = nanoid();

            const timeout = setTimeout(() => {
                pendingRequests.delete(requestId);
                reject(new Error('Timeout'));
            }, HOUR);

            pendingRequests.set(requestId, { resolve, reject, timeout });
            mainWindow.webContents.send('renderer:request-char-path', { requestId });
        });
    },

    closeCharPathSelector: async () => {
        const { mainWindow } = await import('@main/window');
        mainWindow.webContents.send('renderer:close-char-path-selector');
    }
};