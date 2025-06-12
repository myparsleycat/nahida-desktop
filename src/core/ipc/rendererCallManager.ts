// src/core/ipc/rendererCallManager.ts
import { nanoid } from 'nanoid';
import { mainWindow } from '@main/window';

interface PendingRequest {
    resolve: (value: any) => void;
    reject: (reason: any) => void;
    timeout: NodeJS.Timeout;
}

class RendererCallManagerClass {
    private pendingRequests = new Map<string, PendingRequest>();
    // private requestCounter = 0;

    private generateRequestId(): string {
        // return `req_${Date.now()}_${++this.requestCounter}`;
        return nanoid();
    }

    async getSelectedCharPath(): Promise<string> {
        return new Promise((resolve, reject) => {
            const requestId = this.generateRequestId();

            const timeout = setTimeout(() => {
                this.pendingRequests.delete(requestId);
                reject(new Error('Timeout'));
            }, 10000);

            this.pendingRequests.set(requestId, { resolve, reject, timeout });

            mainWindow.webContents.send('renderer:request-char-path', { requestId });
        });
    }

    resolveRequest(requestId: string, result: any): void {
        const pending = this.pendingRequests.get(requestId);
        if (pending) {
            clearTimeout(pending.timeout);
            this.pendingRequests.delete(requestId);

            if (result.success) {
                pending.resolve(result.data);
            } else {
                pending.reject(new Error(result.error));
            }
        }
    }
}

export const RendererCallManager = new RendererCallManagerClass();