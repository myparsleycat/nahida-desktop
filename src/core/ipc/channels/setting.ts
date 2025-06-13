// src/core/ipc/channels/settings.ts
import type { SettingService } from '@core/services/setting.service';
import { ChannelGroup } from '../registry';
import type { languages } from '@shared/types/setting.types';

export function defineSettingChannels(rootGroup: ChannelGroup) {
    const settingsGroup = rootGroup.addGroup('setting');

    const generalGroup = settingsGroup.addGroup('general');
    const generalLangGroup = generalGroup.addGroup('lang');
    generalLangGroup.addChannel('get');
    generalLangGroup.addChannel('set');

    const autofixGroup = settingsGroup.addGroup('autofix');
    const autofixNahidaGroup = autofixGroup.addGroup('nahida');
    autofixNahidaGroup.addChannel('set');
    autofixNahidaGroup.addChannel('get');
}

export function injectSettingHandlers(registry: any, sv: typeof SettingService) {
    registry.injectHandlers('setting.general.lang', {
        get: () => sv.general.lang.get(),
        set: (lang: languages) => sv.general.lang.set(lang)
    })
    registry.injectHandlers('setting.autofix.nahida', {
        set: (value: boolean) => sv.autofix.nahida.set(value),
        get: () => sv.autofix.nahida.get()
    });
}