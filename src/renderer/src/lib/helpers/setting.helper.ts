import type { languages } from "@shared/types/setting.types";
import { locale } from "svelte-i18n";


class SettingHelperClass {
    general = {
        lang: {
            get: () => window.api.setting.general.lang.get(),
            set: async (lang: languages) => {
                await window.api.setting.general.lang.set(lang);
                await locale.set(lang);
            }
        }
    }
}

export const SettingHelper = new SettingHelperClass();