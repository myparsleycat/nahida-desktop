// src/core/ipc/channels/renderer.ts
import { ChannelGroup } from '../registry';

export function defineRendererChannels(rootGroup: ChannelGroup) {
    const rendererGroup = rootGroup.addGroup('renderer');

    rendererGroup.addChannel('requestCharPath', undefined, true, 'renderer:request-char-path');
    rendererGroup.addChannel('requestUserData', undefined, true, 'renderer:request-user-data');

    rendererGroup.addChannel('charPathResponse');
    rendererGroup.addChannel('userDataResponse');
}

let pendingRequests = new Map<string, any>();
let requestCounter = 0;

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
        },
    });
}

export const RendererCallManager = {
    async getSelectedCharPath(): Promise<string> {
        const { mainWindow } = await import('../../../main/window');

        return new Promise((resolve, reject) => {
            const requestId = `req_${Date.now()}_${++requestCounter}`;

            const timeout = setTimeout(() => {
                pendingRequests.delete(requestId);
                reject(new Error('Timeout'));
            }, 10000);

            pendingRequests.set(requestId, { resolve, reject, timeout });
            mainWindow.webContents.send('renderer:request-char-path', { requestId });
        });
    }
};