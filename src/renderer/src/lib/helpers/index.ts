// src/renderer/src/lib/helpers/index.ts

import { writable } from "svelte/store";
import { Auth } from "./auth.helper";
import { NDH } from "./drive.helper";
import { ModsHelper } from "./mods.helper";
import { SettingHelper } from "./setting.helper";
import { toast as svsonner, type ExternalToast } from "svelte-sonner";

type Pages = "mods" | "nahida" | "cloud" | "setting"

class MainHelper {
  page = writable<Pages>("mods");

  async cleanupChannel(channel: string) {
    window.electron.ipcRenderer.removeAllListeners(channel)
  }
}

const Main = new MainHelper();

export {
  Main,
  ModsHelper,
  Auth,
  NDH,
  SettingHelper
}