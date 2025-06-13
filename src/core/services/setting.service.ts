import { db } from "@core/db";
import { ToastService } from "./toast.service";
import type { languages } from "@shared/types/setting.types";


class SettingServiceClass {
    general = {
        lang: {
            get: async () => {
                try {
                    return (await db.get('LocalStorage', 'language'))!;
                } catch (e: any) {
                    console.error('setting.general.lang.get Error', e);
                    ToastService.error('언어 조회중 오류 발생', {
                        description: e.message
                    });
                    return '';
                }
            },
            set: async (lang: languages) => {
                try {
                    await db.update('LocalStorage', 'language', lang);
                    return true;
                } catch (e: any) {
                    console.error('setting.general.lang.set Error', e);
                    ToastService.error('언어 저장중 오류 발생', {
                        description: e.message
                    });
                    return false;
                }
            }
        }
    }

    autofix = {
        nahida: {
            set: async (value: boolean) => {
                try {
                    await db.update('LocalStorage', 'autofix_after_nahida_download', value);
                    return true;
                } catch (e: any) {
                    ToastService.error('설정 변경 중 오류 발생', {
                        description: e.message
                    });
                    return false;
                }
            },
            get: async () => {
                try {
                    return (await db.get('LocalStorage', 'autofix_after_nahida_download'))!;
                } catch (e: any) {
                    ToastService.error('설정 조회중 오류 발생', {
                        description: e.message
                    });
                    return false;
                }
            }
        }
    }
}

export const SettingService = new SettingServiceClass();