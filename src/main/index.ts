// src/main/index.ts

import { app, shell, BrowserWindow, ipcMain, session, dialog, protocol, crashReporter } from 'electron'
import path from 'node:path'
import fs from 'node:fs'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'
import icon from '../../resources/nahida.png?asset'
import { db } from '../core/db'
import { auth } from '../core/services'
import { registerServices } from '../core/ipc-channels'
import { autoUpdater } from 'electron-updater';
import ProgressBar from 'electron-progressbar';
import { NahidaProtocolHandler } from '../core/nahida.protocol'
import { CrashReportUrl } from '../core/const';
import server from '../core/server';

let mainWindow: BrowserWindow;
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

if (process.platform === 'win32') {
  const gotTheLock = app.requestSingleInstanceLock();

  if (!gotTheLock) {
    app.quit();
  } else {
    app.on('second-instance', (_event, commandLine, _workingDirectory) => {
      const deepLinkUrl = commandLine.find((arg) => arg.startsWith('nahida://'));
      if (!deepLinkUrl) return;

      if (deepLinkUrl.startsWith("nahida://auth")) {
        auth.handleOAuth2Callback(deepLinkUrl);
      }

      const windows = BrowserWindow.getAllWindows();
      if (windows.length > 0) {
        if (windows[0].isMinimized()) windows[0].restore();
        windows[0].focus();
      }
    });
  }
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

async function createWindow() {
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
      preload: path.join(__dirname, '../preload/index.js'),
      sandbox: false
    },
    icon
  });

  mainWindow.on('ready-to-show', async () => {
    mainWindow.show();
    autoUpdater.checkForUpdates();
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
        mainWindow.close();
        break;
    }
  });

  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL']);
    // mainWindow.webContents.openDevTools();
  } else {
    mainWindow.loadFile(path.join(__dirname, '../renderer/index.html'));
  }
}

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.whenReady().then(async () => {
  await oneTimeInit();

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
  createWindow();

  // 오버레이
  // createOverlayWindow('Zenless');

  app.on('activate', function () {
    // On macOS it's common to re-create a window in the app when the
    // dock icon is clicked and there are no other windows open.
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
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
  if (!progressBar) return; // progressBar가 없으면 무시

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