import { is } from '@electron-toolkit/utils';
import { BrowserWindow, globalShortcut } from 'electron'
import { OverlayController, OVERLAY_WINDOW_OPTS } from 'electron-overlay-window';
import path from 'path';

let window: BrowserWindow

const toggleMouseKey = 'Ctrl + J'
const toggleShowKey = 'Ctrl + K'

export function createOverlayWindow(windowName: string) {
  window = new BrowserWindow({
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
      preload: path.join(__dirname, '../preload/index.mjs'),
      sandbox: false
    },
    // ...OVERLAY_WINDOW_OPTS
  })

  if (is.dev && process.env['ELECTRON_RENDERER_URL']) {
    window.loadURL(`${process.env['ELECTRON_RENDERER_URL']}/overlay/index.html`);
    window.webContents.openDevTools();
  } else {
    window.loadFile(path.join(__dirname, '../renderer/overlay/index.html'));
  }

  makeDemoInteractive();

  OverlayController.attachByTitle(
    window,
    windowName
  );

  window.on("blur", () => {
    const [w, h] = window.getSize();
    window.setSize(w, h + 1);
    window.setSize(w, h);
  });
  window.on("focus", () => {
    const [w, h] = window.getSize();
    window.setSize(w, h + 1);
    window.setSize(w, h);
  });
}

function makeDemoInteractive() {
  let isInteractable = false

  function toggleOverlayState() {
    if (isInteractable) {
      isInteractable = false
      OverlayController.focusTarget()
      window.webContents.send('focus-change', false)
    } else {
      isInteractable = true
      OverlayController.activateOverlay()
      window.webContents.send('focus-change', true)
    }
  }

  window.on('blur', () => {
    isInteractable = false
    window.webContents.send('focus-change', false)
  })

  globalShortcut.register(toggleMouseKey, toggleOverlayState)

  globalShortcut.register(toggleShowKey, () => {
    window.webContents.send('visibility-change', false)
  })
}
