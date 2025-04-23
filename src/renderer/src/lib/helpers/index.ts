import { writable } from "svelte/store";
import { Auth } from "./auth.helper";
import { Cloud } from "./cloud.helper";
import { Mods } from "./mods.helper";

type Pages = "mods" | "cloud" | "setting"

class MainHelper {
  page = writable<Pages>("mods");

  async cleanupChannel(channel: string) {
    window.electron.ipcRenderer.removeAllListeners(channel)
  }
}

const Main = new MainHelper();

export {
  Main,
  Mods,
  Auth,
  Cloud
}