import { toast, type ExternalToast } from "svelte-sonner";
import { ModsHelper } from "./lib/helpers";
import { CharPathSelector } from "./lib/stores/global.store";

class ListenersManagerClass {
    private listeners: (() => void)[] = [];

    async init(): Promise<void> {
        this.clean();

        const folderPathListener = window.api.mods.msg.currentFolderPathChanged(
            (resp) => {
                ModsHelper.currentFolderPath.set(resp);
            },
        );
        this.listeners.push(folderPathListener);

        const charPathListener = window.api.mods.msg.currentCharPathChanged(
            (resp) => {
                ModsHelper.currentCharPath.set(resp);
            },
        );
        this.listeners.push(charPathListener);

        const toastListener = window.api.toast.toastShow(
            (params: {
                message: string;
                type?: "default" | "success" | "error" | "warning" | "info";
                data?: ExternalToast;
            }) => {
                const { message, type = "default", data } = params;

                switch (type) {
                    case "success":
                        toast.success(message, data);
                        break;
                    case "error":
                        toast.error(message, data);
                        break;
                    case "warning":
                        toast.warning(message, data);
                        break;
                    case "info":
                        toast.info(message, data);
                        break;
                    default:
                        toast(message, data);
                }
            },
        );
        this.listeners.push(toastListener);

        const charPathRequestListener = window.api.renderer.requestCharPath(
            async (data: any) => {
                const { requestId } = data;
                const result = await CharPathSelector.open();

                window.api.renderer.charPathResponse(requestId, {
                    success: true,
                    data: result,
                });
            },
        );
        this.listeners.push(charPathRequestListener);

        const closeCharPathSelectorListener =
            window.api.renderer.closeCharPathSelector(async () => {
                CharPathSelector.close();
            });
        this.listeners.push(closeCharPathSelectorListener);
    }

    clean(): void {
        this.listeners.forEach((removeListener) => {
            removeListener();
        });
        this.listeners = [];
    }

    destroy(): void {
        this.clean();
    }
}

export const ListenersManager = new ListenersManagerClass();