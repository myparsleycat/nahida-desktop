let minimizeListenerSet = false;
let maximizeListenerSet = false;
let closeListenerSet = false;

export function setupWindowControls(): void {
    const minimizeBtn = document.getElementById("minimizeBtn");
    const maximizeBtn = document.getElementById("maximizeBtn");
    const closeBtn = document.getElementById("closeBtn");

    if (minimizeBtn && !minimizeListenerSet) {
        minimizeBtn.addEventListener("click", () => {
            window.electron.ipcRenderer.send("window-control", "minimize");
        });
        minimizeListenerSet = true;
    }

    if (maximizeBtn && !maximizeListenerSet) {
        maximizeBtn.addEventListener("click", () => {
            window.electron.ipcRenderer.send("window-control", "maximize");
        });
        maximizeListenerSet = true;
    }

    if (closeBtn && !closeListenerSet) {
        closeBtn.addEventListener("click", () => {
            window.electron.ipcRenderer.send("window-control", "close");
        });
        closeListenerSet = true;
    }
}
