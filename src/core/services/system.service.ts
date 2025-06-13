import psList from 'ps-list';
import { activeWindow } from 'get-windows';

class SystemServiceClass {
    async getProcessList() {
        return psList();
    }

    async getActiveWindow() {
        return activeWindow();
    }
}

export const SystemService = new SystemServiceClass();