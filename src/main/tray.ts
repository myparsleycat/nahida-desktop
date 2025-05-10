import { app, Menu, Tray } from "electron";
import icon from '../../resources/nahida.png?asset'
import { mainWindow } from "./window";

function createTray() {
  const tray = new Tray(icon);
  const contextMenu = Menu.buildFromTemplate([
    { label: '항목1', type: 'normal', click: () => { /* 동작 */ } },
    { type: 'separator' },
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