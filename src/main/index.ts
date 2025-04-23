// src/main/index.ts

import { app, shell, BrowserWindow, ipcMain, session, dialog } from 'electron'
import path from 'node:path'
import { electronApp, optimizer, is } from '@electron-toolkit/utils'
import icon from '../../resources/puhaha.png?asset'
import { db } from '../core/db'
import { auth } from '../core/services'
import { registerServices } from '../core/ipc-channels'
import { autoUpdater } from 'electron-updater';
import ProgressBar from 'electron-progressbar';

let mainWindow: BrowserWindow;
let progressBar: ProgressBar;

// 딥링크
if (process.defaultApp) {
  if (process.argv.length >= 2) {
    app.setAsDefaultProtocolClient('nahida', process.execPath, [path.resolve(process.argv[1])])
  }
} else {
  app.setAsDefaultProtocolClient('nahida')
}

if (process.platform === 'win32') {
  const gotTheLock = app.requestSingleInstanceLock()

  if (!gotTheLock) {
    app.quit()
  } else {
    app.on('second-instance', (_event, commandLine, _workingDirectory) => {
      const deepLinkUrl = commandLine.find((arg) => arg.startsWith('nahida://'));

      if (deepLinkUrl) {
        auth.handleOAuth2Callback(deepLinkUrl);
      }

      const windows = BrowserWindow.getAllWindows();
      if (windows.length > 0) {
        if (windows[0].isMinimized()) windows[0].restore();
        windows[0].focus();
      }
    })
  }
}

async function oneTimeInit() {
  await db.init();
}

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1000,
    height: 670,
    minWidth: 800,
    minHeight: 550,
    show: false,
    frame: false,
    autoHideMenuBar: true,
    ...(process.platform === 'linux' ? { icon } : {}),
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
      sandbox: false
    }
  })

  mainWindow.on('ready-to-show', async () => {
    mainWindow.show();
    autoUpdater.checkForUpdates();
  })

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url)
    return { action: 'deny' }
  })

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

  // HMR for renderer base on electron-vite cli.
  // Load the remote URL for development or the local html file for production.
  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    mainWindow.loadURL(process.env['ELECTRON_RENDERER_URL'])
  } else {
    mainWindow.loadFile(path.join(__dirname, '../renderer/index.html'))
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
  })

  // Set app user model id for windows
  electronApp.setAppUserModelId('com.nahida')

  // Default open or close DevTools by F12 in development
  // and ignore CommandOrControl + R in production.
  // see https://github.com/alex8088/electron-toolkit/tree/master/packages/utils
  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window)
  })

  // ipc
  ipcMain.on('ping', () => console.log('pong'))
  registerServices(ipcMain);

  createWindow()

  app.on('activate', function () {
    // On macOS it's common to re-create a window in the app when the
    // dock icon is clicked and there are no other windows open.
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

// 자동으로 업데이트가 되는 것 방지
autoUpdater.autoDownload = false;

autoUpdater.on("checking-for-update", () => {
  console.log("업데이트 확인 중");
});

autoUpdater.on("update-available", () => {
  console.log("업데이트 버전 확인");

  dialog
    .showMessageBox({
      type: "info",
      title: "Update",
      message:
        "새로운 버전이 확인되었습니다. 설치 파일을 다운로드 하시겠습니까?",
      buttons: ["지금 설치", "나중에 설치"]
    })
    .then(result => {
      const { response } = result;

      if (response === 0) autoUpdater.downloadUpdate();
    });
});

autoUpdater.on("update-not-available", () => {
  console.log("업데이트 불가");
});

autoUpdater.once("download-progress", () => {
  console.log("설치 중");

  progressBar = new ProgressBar({
    text: "Download 합니다."
  });

  progressBar
    .on("completed", () => {
      console.log("설치 완료");
    })
    .on("aborted", () => {
      console.log("aborted");
    });
});

autoUpdater.on("update-downloaded", () => {
  console.log("업데이트 완료");

  progressBar.setCompleted();

  dialog
    .showMessageBox({
      type: "info",
      title: "Update",
      message: "새로운 버전이 다운로드 되었습니다. 다시 시작하시겠습니까?",
      buttons: ["예", "아니오"]
    })
    .then(result => {
      const { response } = result;

      if (response === 0) autoUpdater.quitAndInstall(false, true);
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