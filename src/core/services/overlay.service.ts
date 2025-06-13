import { is } from '@electron-toolkit/utils';
import { app, BrowserWindow, globalShortcut } from 'electron';
import { OverlayController, OVERLAY_WINDOW_OPTS } from 'electron-overlay-window';
import path from 'node:path';

const toggleMouseKey = 'Ctrl + A'
const toggleShowKey = 'Ctrl + A'

class OverlayServiceClass {
    public window: BrowserWindow | null = null;
    private readonly PRELOAD = path.join(__dirname, '../preload/index.mjs');
    private isInteractable = false;

    async createOverlayWindow() {
        this.window = new BrowserWindow({
            width: 1280,
            height: 720,
            frame: false,
            transparent: true,
            backgroundColor: '#00000000',
            alwaysOnTop: true,
            focusable: false,
            skipTaskbar: true,
            hasShadow: false,
            type: 'toolbar',
            autoHideMenuBar: true,
            show: false,
            resizable: false,
            thickFrame: false,
            roundedCorners: false,
            useContentSize: true,
            minimizable: false,
            webPreferences: {
                nodeIntegration: true,
                contextIsolation: false,
                preload: this.PRELOAD,
                sandbox: false
            },
        });

        if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
            this.window.loadURL(`${process.env['ELECTRON_RENDERER_URL']}/overlay/index.html`);
            this.window.webContents.openDevTools();
        } else {
            this.window.loadFile(path.join(__dirname, '../renderer/overlay/index.html'));
        }

        this.makeDemoInteractive();

        OverlayController.attachByTitle(
            this.window,
            'Zenless'
        );

        this.window.on("blur", () => {
            if (!this.window) return;
            const [w, h] = this.window.getSize();
            this.window.setSize(w, h + 1);
            this.window.setSize(w, h);
        });

        this.window.on("focus", () => {
            if (!this.window) return;
            const [w, h] = this.window.getSize();
            this.window.setSize(w, h + 1);
            this.window.setSize(w, h);
        });
    }

    toggleOverlayState() {
        if (this.isInteractable) {
            this.isInteractable = false
            OverlayController.focusTarget()
            this.window?.webContents.send('focus-change', false)
        } else {
            this.isInteractable = true
            OverlayController.activateOverlay()
            this.window?.webContents.send('focus-change', true)
        }
    }

    makeDemoInteractive() {
        if (!this.window) return;

        this.window.on('blur', () => {
            this.isInteractable = false
            this.window?.webContents.send('focus-change', false)
        })

        globalShortcut.register(toggleMouseKey, this.toggleOverlayState)

        globalShortcut.register(toggleShowKey, () => {
            this.window?.webContents.send('visibility-change', false)
        })
    }

}

export const OverlayService = new OverlayServiceClass();