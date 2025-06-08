// src/core/ipc/channels/toast.ts
import { ChannelGroup } from '../registry';
import { ToastService, ToastMessage } from '@core/services/toast.service';
import type { ExternalToast } from "svelte-sonner";

export function defineToastChannels(rootGroup: ChannelGroup) {
    const toastGroup = rootGroup.addGroup('toast');
    toastGroup.addChannel('show');
    toastGroup.addChannel('success');
    toastGroup.addChannel('error');
    toastGroup.addChannel('warning');
    toastGroup.addChannel('info');
    toastGroup.addChannel('toastShow', undefined, true, 'toast-show');
}

export function injectToastHandlers(registry: any) {
    registry.injectHandlers('toast', {
        show: (message: string, type?: ToastMessage['type'], data?: ExternalToast) => ToastService.show(message, type, data),
        success: (message: string, data?: ExternalToast) => ToastService.success(message, data),
        error: (message: string, data?: ExternalToast) => ToastService.error(message, data),
        warning: (message: string, data?: ExternalToast) => ToastService.warning(message, data),
        info: (message: string, data?: ExternalToast) => ToastService.info(message, data)
    });
}