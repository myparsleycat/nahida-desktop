import type { AuthService, DriveService, FSService, ModsService, NahidaService, SettingService } from "@core/services";

// src/core/ipc/types.ts
export interface Services {
  AuthService: typeof AuthService;
  FSService: typeof FSService;
  DriveService: typeof DriveService;
  ModsService: typeof ModsService;
  NahidaService: typeof NahidaService;
  SettingService: typeof SettingService;
}