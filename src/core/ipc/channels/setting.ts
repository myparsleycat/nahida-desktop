// src/core/ipc/channels/settings.ts
import type { SettingService } from '@core/services/setting.service';
import { ChannelGroup } from '../registry';

export function defineSettingChannels(rootGroup: ChannelGroup) {
    const settingsGroup = rootGroup.addGroup('setting');
    const autofixGroup = settingsGroup.addGroup('autofix');
    const autofixNahidaGroup = autofixGroup.addGroup('nahida');
    autofixNahidaGroup.addChannel('set');
    autofixNahidaGroup.addChannel('get');
}

export function injectSettingHandlers(registry: any, sv: typeof SettingService) {
    registry.injectHandlers('setting.autofix.nahida', {
        set: (value: boolean) => sv.autofix.nahida.set(value),
        get: () => sv.autofix.nahida.get()
    });
}