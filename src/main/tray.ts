import { app, Menu, Tray } from "electron";
import icon from '../../resources/nahida.png?asset'
import { mainWindow } from "./window";
import { getAutoUpdater } from "./updater";

function createTray() {
  const tray = new Tray(icon);
  const contextMenu = Menu.buildFromTemplate([
    { label: 'Check Update', type: 'normal', click: async () => { const updater = getAutoUpdater(); await updater.checkForUpdates() } },
    // { type: 'separator' },
    { label: 'Quit', type: 'normal', click: () => { app.quit(); } }
  ]);
  tray.setToolTip('Nahida Desktop');
  tray.setContextMenu(contextMenu);
  tray.on('click', () => {
    mainWindow.show();
  });
  return tray;
}

export { createTray };