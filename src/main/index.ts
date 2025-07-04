// src/main/index.ts

import { app, BrowserWindow, ipcMain, dialog, protocol, crashReporter } from 'electron';
import path from 'node:path';
import { electronApp, optimizer, is } from '@electron-toolkit/utils';
import { AuthService, ToastService } from '@core/services';
import { NahidaProtocolHandler } from '@core/nahida.protocol';
import { CrashReportUrl } from '@core/const';
import { createMainWindow, mainWindow } from './window';
import { registerServices } from '@core/ipc';
import AutoLaunch from 'auto-launch';
import { getAutoUpdater } from './updater';
import log from 'electron-log';
import { startInit } from './init';

console.log = log.log;
console.error = log.error;
console.info = log.info;

crashReporter.start({ submitURL: CrashReportUrl });

// 딥링크
if (process.defaultApp) {
    if (process.argv.length >= 2) {
        app.setAsDefaultProtocolClient('nahida', process.execPath, [path.resolve(process.argv[1])]);
    }
} else {
    app.setAsDefaultProtocolClient('nahida');
}

function registerCustomProtocol() {
    protocol.handle('nahida', async (req) => await NahidaProtocolHandler(req))
}

const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
    log.warn('앱이 이미 실행중임');
    app.quit();
} else {
    app.on('second-instance', (_event, commandLine, _workingDirectory) => {
        const deepLinkUrl = commandLine.find((arg) => arg.startsWith('nahida://'));
        if (deepLinkUrl && deepLinkUrl.startsWith("nahida://auth")) {
            AuthService.handleOAuth2Callback(deepLinkUrl);
        }

        if (mainWindow) {
            if (mainWindow.isMinimized()) mainWindow.restore();
            mainWindow.show();
            mainWindow.focus();
        }
    });

    // This method will be called when Electron has finished
    // initialization and is ready to create browser windows.
    // Some APIs can only be used after this event occurs.
    app.whenReady().then(async () => {
        await startInit();

        app.on('open-url', (_, url) => {
            // dialog.showErrorBox('Welcome Back', `You arrived from: ${url}`)
            AuthService.handleOAuth2Callback(url);
        });

        // Set app user model id for windows
        electronApp.setAppUserModelId('com.nahida');

        // Default open or close DevTools by F12 in development
        // and ignore CommandOrControl + R in production.
        // see https://github.com/alex8088/electron-toolkit/tree/master/packages/utils
        app.on('browser-window-created', (_, window) => {
            optimizer.watchWindowShortcuts(window);
        });

        // ipc
        registerServices(ipcMain);

        registerCustomProtocol();
        const createdMainWindow = await createMainWindow();
        createdMainWindow.on('ready-to-show', async () => {
            const autoUpdater = getAutoUpdater();
            await autoUpdater.checkForUpdates();
            ToastService.setMainWindow(createdMainWindow);
        })

        // 오버레이
        // createOverlayWindow('Zenless');

        app.on('activate', async () => {
            // On macOS it's common to re-create a window in the app when the
            // dock icon is clicked and there are no other windows open.
            if (BrowserWindow.getAllWindows().length === 0) await createMainWindow();
        });
    })

    app.on('ready', () => {
        if (app.isPackaged) {
            const autoLaunch = new AutoLaunch({
                name: 'Nahida Desktop',
                path: app.getPath('exe'),
                isHidden: true
            });
            autoLaunch.isEnabled().then((isEnabled) => {
                if (!isEnabled) autoLaunch.enable();
            });
        }
    })

    // Quit when all windows are closed, except on macOS. There, it's common
    // for applications and their menu bar to stay active until the user quits
    // explicitly with Cmd + Q.
    app.on('window-all-closed', () => {
        if (process.platform !== 'darwin') {
            app.quit()
        }
    })

    // In this file you can include the rest of your app's specific main process
    // code. You can also put them in separate files and require them here.
}