import { db } from "@core/db";
import { ToastService } from "./toast.service";


class SettingServiceClass {
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