import { BrowserWindow, ipcMain, shell } from "electron";
import { db } from "@core/db";
import path from "node:path";
import icon from '../../resources/nahida.png?asset'
import { is } from "@electron-toolkit/utils";
import { fileURLToPath } from "node:url";

export let mainWindow: BrowserWindow;

export async function createMainWindow() {
    const bounds = await db.get('LocalStorage', 'bounds');
    mainWindow = new BrowserWindow({
        x: bounds?.x || undefined,
        y: bounds?.y || undefined,
        width: bounds?.width || 1000,
        height: bounds?.height || 670,
        minWidth: 800,
        minHeight: 550,
        show: false,
        frame: false,
        autoHideMenuBar: true,
        ...(process.platform === 'linux' ? { icon } : {}),
        webPreferences: {
            preload: fileURLToPath(new URL('../preload/index.mjs', import.meta.url)),
            sandbox: false
        },
        icon
    });

    mainWindow.on('ready-to-show', async () => {
        mainWindow.show();
    });

    mainWindow.on('close', async () => {
        const bounds = mainWindow.getBounds();
        await db.update('LocalStorage', 'bounds', bounds);
    });

    mainWindow.webContents.setWindowOpenHandler((details) => {
        shell.openExternal(details.url)
        return { action: 'deny' }
    });

    ipcMain.on('window-control', (_, command) => {
        switch (command) {
            case 'minimize':
                mainWindow.minimize();
                break;
            case 'maximize':
                if (mainWindow.isMaximized()) {
                    mainWindow.unmaximize();
                } else {
                    mainWindow.maximize();
                }
                break;
            case 'close':
                mainWindow.hide();
                break;
        }
    });

    if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
        mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL']);
        // mainWindow.webContents.openDevTools();
    } else {
        mainWindow.loadFile(path.join(__dirname, '../renderer/index.html'));
    }

    return mainWindow;
}

export function focus() {
    mainWindow.show();
    mainWindow.moveTop();
    mainWindow.focus();
}