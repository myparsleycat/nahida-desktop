// src/main/index.ts

import { app, BrowserWindow, ipcMain, dialog, protocol, crashReporter } from 'electron';
import path from 'node:path';
import { electronApp, optimizer, is } from '@electron-toolkit/utils';
import { db } from '@core/db';
import { auth, Toast } from '@core/services';
import ElectronUpdater from 'electron-updater';
const { autoUpdater } = ElectronUpdater;
import ProgressBar from 'electron-progressbar';
import { NahidaProtocolHandler } from '@core/nahida.protocol';
import { CrashReportUrl } from '@core/const';
import server from '@core/server';
import { createTray } from './tray';
import { createMainWindow, mainWindow } from './window';
import { registerServices } from '@core/ipc';
// import { createOverlayWindow } from '../core/overlay';

let progressBar: ProgressBar | null = null;
let initialized = false;

crashReporter.start({ submitURL: CrashReportUrl });

// 딥링크
if (process.defaultApp) {
  if (process.argv.length >= 2) {
    app.setAsDefaultProtocolClient('nahida', process.execPath, [path.resolve(process.argv[1])]);
  }
} else {
  app.setAsDefaultProtocolClient('nahida');
}

async function oneTimeInit() {
  if (initialized) return;
  await db.init();
  server.listen(14327, ({ hostname, port }) => {
    console.log(`server is running at ${hostname}:${port}`)
  });
  initialized = true;
}

function registerCustomProtocol() {
  protocol.handle('nahida', async (req) => await NahidaProtocolHandler(req))
}

const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  console.log('앱이 이미 실행 중임');
  app.quit();
} else {
  app.on('second-instance', (_event, commandLine, _workingDirectory) => {
    const deepLinkUrl = commandLine.find((arg) => arg.startsWith('nahida://'));
    if (deepLinkUrl && deepLinkUrl.startsWith("nahida://auth")) {
      auth.handleOAuth2Callback(deepLinkUrl);
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
    await oneTimeInit();
    createTray();

    app.on('open-url', (_, url) => {
      // dialog.showErrorBox('Welcome Back', `You arrived from: ${url}`)
      auth.handleOAuth2Callback(url);
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
    ipcMain.on('ping', () => console.log('pong'));
    registerServices(ipcMain);

    registerCustomProtocol();
    const createdMainWindow = await createMainWindow();
    createdMainWindow.on('ready-to-show', async () => {
      await autoUpdater.checkForUpdates();
      Toast.setMainWindow(createdMainWindow);
    })

    // 오버레이
    // createOverlayWindow('Zenless');

    app.on('activate', async () => {
      // On macOS it's common to re-create a window in the app when the
      // dock icon is clicked and there are no other windows open.
      if (BrowserWindow.getAllWindows().length === 0) await createMainWindow();
    });
  })

  autoUpdater.autoDownload = false;

  autoUpdater.on("checking-for-update", () => {
    console.log("업데이트 확인 중");
  });

  autoUpdater.on("update-available", (au) => {
    console.log("Update version detected");

    dialog
      .showMessageBox({
        type: "info",
        title: `New Update Available: v${au.version}`,
        message:
          "새로운 버전으로 업데이트 할 수 있습니다. 지금 진행할까요?",
        buttons: ["확인", "나중에 진행"]
      })
      .then(result => {
        const { response } = result;

        if (response === 0) {
          progressBar = new ProgressBar({
            detail: 'Wait...',
            text: "Download Files...",
            initialValue: 0,
            maxValue: 100
          });

          progressBar
            .on("completed", () => {
              console.info(`completed...`);
              if (progressBar) progressBar.detail = 'Update completed. Closing...';
            })
            .on("aborted", () => {
              console.log("aborted");
            })
            .on("progress", function (percent: number) {
              if (progressBar) progressBar.text = `Download Files... ${percent}%`;
            });

          autoUpdater.downloadUpdate();
        }
      });
  });

  autoUpdater.on("update-not-available", () => {
    console.log("업데이트 불가");
  });

  autoUpdater.on("download-progress", (pg) => {
    if (!progressBar) return;

    const percent = Math.floor(pg.percent);
    progressBar.value = percent;
    progressBar.text = `Download Files... ${percent}%`;
  });

  autoUpdater.on("update-downloaded", () => {
    if (progressBar) {
      progressBar.setCompleted();
    }

    dialog
      .showMessageBox({
        type: "info",
        title: "Update",
        message: "새로운 버전이 다운로드 되었습니다. 다시 시작할까요?",
        buttons: ["Yes", "No"]
      })
      .then(result => {
        const { response } = result;
        if (response === 0) autoUpdater.quitAndInstall(true, true);
      });
  });

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