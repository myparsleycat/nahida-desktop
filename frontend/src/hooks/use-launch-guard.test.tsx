// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  StartGame: vi.fn(),
  StartGameWithLogging: vi.fn(),
  DisableLogging: vi.fn(),
  ClearLaunchBlockers: vi.fn(),
  GetImporterConfig: vi.fn(),
  SaveImporterConfig: vi.fn(),
  DetectGameFolders: vi.fn(),
  ValidateGameFolder: vi.fn(),
  RepairRuntime: vi.fn(),
  LaunchUpdates: vi.fn(),
  InstallUpdates: vi.fn(),
  LaunchPresetEffects: vi.fn(),
  LaunchUncompressedTextures: vi.fn(),
  CompressLaunchTextures: vi.fn(),
}));
const reshade = vi.hoisted(() => ({ InstallEffectPackages: vi.fn() }));
const navigate = vi.hoisted(() => vi.fn());

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("@bindings/reshade", () => ({ ReShade: reshade }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => () => {} } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: undefined }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), info: vi.fn(), warning: vi.fn() } }));

import { toast } from "sonner";

import {
  launchDialog,
  launchErrorCode,
  smoothMotionDLLOutdated,
  useLaunchGuard,
} from "./use-launch-guard";

const toastError = vi.mocked(toast.error);
const toastWarning = vi.mocked(toast.warning);

beforeEach(() => {
  xxmi.LaunchUpdates.mockResolvedValue([]);
  xxmi.LaunchPresetEffects.mockResolvedValue({ packages: [], unknown: [] });
  xxmi.LaunchUncompressedTextures.mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  xxmi.LaunchUpdates.mockReset();
  xxmi.InstallUpdates.mockReset();
  xxmi.LaunchPresetEffects.mockReset();
  xxmi.LaunchUncompressedTextures.mockReset();
  xxmi.CompressLaunchTextures.mockReset();
  reshade.InstallEffectPackages.mockReset();
  toastWarning.mockClear();
  xxmi.StartGame.mockReset();
  xxmi.StartGameWithLogging.mockReset();
  xxmi.DisableLogging.mockReset();
  xxmi.ClearLaunchBlockers.mockReset();
  xxmi.GetImporterConfig.mockReset();
  xxmi.SaveImporterConfig.mockReset();
  xxmi.DetectGameFolders.mockReset();
  xxmi.ValidateGameFolder.mockReset();
  xxmi.RepairRuntime.mockReset();
  navigate.mockReset();
  toastError.mockClear();
});

it.each(["XXMI_NOT_CONFIGURED", "XXMI_IMPORTER_NOT_INSTALLED"])(
  "opens importer setup for %s",
  async (code) => {
    xxmi.StartGame.mockRejectedValueOnce(new Error(code));
    navigate.mockResolvedValue(undefined);

    render(<Harness importer="GIMI" />);
    fireEvent.click(screen.getByRole("button", { name: "play" }));

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/xxmi/$importer",
        params: { importer: "GIMI" },
      }),
    );
    expect(screen.queryByRole("alertdialog")).toBeNull();
  },
);

it("picks one launch dialog for the blocker codes", () => {
  expect(launchDialog("GIMI_DCR_ENABLED")).toBe("gimi-dcr");
  expect(launchDialog("NVIDIA_SMOOTH_MOTION_ENABLED")).toBe("smooth-motion");
  expect(launchDialog("GIMI_DCR_ENABLED\nNVIDIA_SMOOTH_MOTION_ENABLED")).toBe("launch-blockers");
  expect(launchDialog("WWMI_WOUNDED_FX_DECISION_REQUIRED")).toBe("wwmi-wounded");
  expect(launchDialog("WWMI_RESOURCE_TIER_DECISION_REQUIRED")).toBe("wwmi-resource-tier");
  expect(launchDialog("XXMI_D3D11_MODE_NOTICE_REQUIRED")).toBe("d3d11-mode");
  expect(launchDialog("XXMI_LOGGING_ENABLED")).toBe("xxmi-logging");
  expect(launchDialog("XXMI_GAME_FOLDER_NOT_CONFIGURED")).toBe("game-folder");
  expect(launchDialog("XXMI_RUNTIME_CORRUPTED")).toBe("runtime-repair");
  expect(launchDialog("XXMI is not configured")).toBeNull();
});

it("reads the versions off an outdated DLL blocker", () => {
  const message =
    "GIMI_DCR_ENABLED\nNVIDIA_SMOOTH_MOTION_ENABLED:XXMI_SMOOTH_MOTION_DLL_OUTDATED:1.2.2-nhd.2:1.2.2-nhd.3";
  expect(launchDialog(message)).toBe("launch-blockers");
  expect(smoothMotionDLLOutdated(message)).toEqual({
    version: "1.2.2-nhd.2",
    since: "1.2.2-nhd.3",
  });
  expect(smoothMotionDLLOutdated("NVIDIA_SMOOTH_MOTION_ENABLED")).toBeNull();
});

it.each([
  "XXMI_BUSY",
  "XXMI_GAME_RUNNING",
  "XXMI_RUNTIME_LOCKED",
  "XXMI_ELEVATION_DENIED",
  "XXMI_LOADER_TOO_OLD",
  "XXMI_INJECT_FAILED",
  "XXMI_GAME_START_TIMEOUT",
  "XXMI_LEGACY_LOADER_RUNNING",
  "XXMI_LEGACY_LOADER_EXITED",
  "XXMI_LEGACY_LOADER_NOT_READY",
  "XXMI_PLATFORM_NOT_FOUND",
  "XXMI_PLATFORM_GAME_NOT_FOUND",
  "XXMI_PLATFORM_OPTIONS_FAILED",
  "GIMI_FPS_UNLOCKER_RUNNING",
  "GIMI_FPS_UNLOCKER_CONFIG_FAILED",
  "GIMI_HDR_CONFIG_FAILED",
  "SRMI_FPS_UNLOCK_FAILED",
  "HIMI_FPS_UNLOCK_FAILED",
  "ZZMI_GAME_CONFIG_FAILED",
  "WWMI_GAME_CONFIG_FAILED",
  "RESHADE_NOT_INSTALLED",
])("shows guidance for %s and keeps the backend detail", async (code) => {
  const detail = `${code}: native code 200`;
  xxmi.StartGame.mockRejectedValueOnce(new Error(detail));

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));

  await waitFor(() =>
    expect(toastError).toHaveBeenCalledWith(`page.setting.xxmi.builtin.launchErrors.${code}`, {
      description: detail,
    }),
  );
  expect(launchErrorCode(detail)).toBe(code);
  expect(screen.queryByRole("alertdialog")).toBeNull();
});

it("reports a declined elevation over the game settings step that asked for it", () => {
  const detail = "ZZMI_GAME_CONFIG_FAILED: XXMI_ELEVATION_DENIED: The operation was canceled";
  expect(launchErrorCode(detail)).toBe("XXMI_ELEVATION_DENIED");
});

it("repairs a damaged runtime before retrying launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(
    new Error("XXMI_RUNTIME_CORRUPTED: C:\\Mods\\GIMI\\d3d11.dll is missing"),
  );
  xxmi.StartGame.mockResolvedValueOnce(undefined);
  xxmi.RepairRuntime.mockResolvedValue([]);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(screen.getByText(/C:\\Mods\\GIMI\\d3d11\.dll is missing/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.repairRuntime" }));

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.RepairRuntime).toHaveBeenCalledWith("GIMI");
});

it("saves a detected game folder and retries launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_GAME_FOLDER_NOT_CONFIGURED"));
  xxmi.StartGame.mockResolvedValueOnce(undefined);
  xxmi.DetectGameFolders.mockResolvedValue([
    { path: "C:\\Games\\Genshin Impact", exePath: "C:\\Games\\Genshin Impact\\GenshinImpact.exe" },
  ]);
  xxmi.ValidateGameFolder.mockResolvedValue(undefined);
  xxmi.GetImporterConfig.mockResolvedValue({ gameFolder: "" });
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.detectGame" }));
  fireEvent.click(await screen.findByRole("button", { name: "C:\\Games\\Genshin Impact" }));
  fireEvent.click(screen.getByRole("button", { name: "g.save" }));

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.ValidateGameFolder).toHaveBeenCalledWith("GIMI", "C:\\Games\\Genshin Impact");
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "GIMI",
    expect.objectContaining({ gameFolder: "C:\\Games\\Genshin Impact" }),
  );
});

it("saves the wounded effect choice before retrying launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("WWMI_WOUNDED_FX_DECISION_REQUIRED"));
  xxmi.GetImporterConfig.mockResolvedValue({ wwmi: { disableWoundedFX: false } });
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.StartGame.mockResolvedValueOnce(undefined);

  render(<Harness importer="WWMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.wwmi-wounded.confirm" }));
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "WWMI",
    expect.objectContaining({ woundedFXDecided: true, wwmi: { disableWoundedFX: true } }),
  );
});

it("saves the resource quality and still asks about the wounded effect", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("WWMI_RESOURCE_TIER_DECISION_REQUIRED"));
  xxmi.StartGame.mockRejectedValueOnce(new Error("WWMI_WOUNDED_FX_DECISION_REQUIRED"));
  xxmi.GetImporterConfig.mockResolvedValue({ wwmi: { resourceTier: "HD" } });
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);

  render(<Harness importer="WWMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "UHD" }));
  fireEvent.click(
    screen.getByRole("button", { name: "page.mod.dialog.wwmi-resource-tier.confirm" }),
  );

  expect(
    await screen.findByRole("button", { name: "page.mod.dialog.wwmi-wounded.confirm" }),
  ).toBeTruthy();
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "WWMI",
    expect.objectContaining({ wwmi: { resourceTier: "UHD", resourceTierDecided: true } }),
  );
  expect(xxmi.StartGame).toHaveBeenCalledTimes(2);
});

it("records the DirectX 11 reminder before retrying launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_D3D11_MODE_NOTICE_REQUIRED"));
  xxmi.GetImporterConfig.mockResolvedValue({ gameLaunch: "Epic", d3d11ModeNoticeShown: false });
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.StartGame.mockResolvedValueOnce(undefined);

  render(<Harness importer="EFMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.d3d11-mode.confirm" }));

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith("EFMI", {
    gameLaunch: "Epic",
    d3d11ModeNoticeShown: true,
  });
});

it("can keep the wounded effect when retrying launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("WWMI_WOUNDED_FX_DECISION_REQUIRED"));
  xxmi.GetImporterConfig.mockResolvedValue({ wwmi: { disableWoundedFX: true } });
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.StartGame.mockResolvedValueOnce(undefined);

  render(<Harness importer="WWMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.wwmi-wounded.keep" }));
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "WWMI",
    expect.objectContaining({ woundedFXDecided: true, wwmi: { disableWoundedFX: false } }),
  );
});

it("turns logging off before retrying launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_LOGGING_ENABLED"));
  xxmi.StartGame.mockResolvedValueOnce(undefined);
  xxmi.DisableLogging.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.xxmi-logging.confirm" }));

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.DisableLogging).toHaveBeenCalledWith("GIMI");
  expect(xxmi.StartGameWithLogging).not.toHaveBeenCalled();
});

it("keeps logging on through the later dialogs of the same launch", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_LOGGING_ENABLED"));
  xxmi.StartGameWithLogging.mockRejectedValueOnce(new Error("NVIDIA_SMOOTH_MOTION_ENABLED"));
  xxmi.StartGameWithLogging.mockResolvedValueOnce(undefined);
  xxmi.ClearLaunchBlockers.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  fireEvent.click(await screen.findByRole("button", { name: "page.mod.dialog.xxmi-logging.keep" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "page.mod.dialog.smooth-motion.confirm" }),
  );

  await waitFor(() => expect(xxmi.StartGameWithLogging).toHaveBeenCalledTimes(2));
  expect(xxmi.StartGameWithLogging).toHaveBeenCalledWith("GIMI");
  expect(xxmi.ClearLaunchBlockers).toHaveBeenCalledWith("GIMI");
  expect(xxmi.DisableLogging).not.toHaveBeenCalled();
  expect(xxmi.StartGame).toHaveBeenCalledTimes(1);
});

it("does not launch when the logging dialog is cancelled", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_LOGGING_ENABLED"));

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));

  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(xxmi.DisableLogging).not.toHaveBeenCalled();
  expect(xxmi.StartGameWithLogging).not.toHaveBeenCalled();
  expect(xxmi.StartGame).toHaveBeenCalledTimes(1);
});

function Harness({ importer = "GIMI" }: { importer?: string }) {
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  return (
    <div>
      <button type="button" onClick={() => void startImporter(importer)}>
        play
      </button>
      {launchGuardDialog}
    </div>
  );
}

it("asks about uncompressed textures once and launches when they are ignored", async () => {
  xxmi.LaunchUncompressedTextures.mockResolvedValue([
    { path: "C:/Mods/A/Diffuse.dds", relativePath: "A/Diffuse.dds", width: 2048, height: 2048 },
  ]);
  xxmi.StartGame.mockResolvedValue(undefined);

  render(<Harness importer="WWMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByText("A/Diffuse.dds");
  expect(xxmi.StartGame).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));
  await waitFor(() => expect(screen.queryByText("A/Diffuse.dds")).toBeNull());
  expect(xxmi.StartGame).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "play" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "page.mod.dialog.uncompressed-textures.ignore" }),
  );
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(1));

  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));
  expect(xxmi.CompressLaunchTextures).not.toHaveBeenCalled();

  // Compressing must leave the ignored texture alone, so only the one on screen is sent.
  xxmi.LaunchUncompressedTextures.mockResolvedValue([
    { path: "C:/Mods/A/Diffuse.dds", relativePath: "A/Diffuse.dds", width: 2048, height: 2048 },
    { path: "C:/Mods/B/Diffuse.dds", relativePath: "B/Diffuse.dds", width: 2048, height: 2048 },
  ]);
  xxmi.CompressLaunchTextures.mockResolvedValue({ files: [] });
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByText("B/Diffuse.dds");
  expect(screen.queryByText("A/Diffuse.dds")).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "page.mod.dialog.uncompressed-textures.confirm" }),
  );
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(3));
  expect(xxmi.CompressLaunchTextures).toHaveBeenCalledWith("WWMI", ["C:/Mods/B/Diffuse.dds"]);
});

it("does not launch when the page unmounts while textures are compressing", async () => {
  xxmi.LaunchUncompressedTextures.mockResolvedValue([
    { path: "C:/Mods/C/Diffuse.dds", relativePath: "C/Diffuse.dds", width: 2048, height: 2048 },
  ]);
  let finish: (result: { files: never[] }) => void = () => {};
  const cancel = vi.fn();
  xxmi.CompressLaunchTextures.mockReturnValue(
    Object.assign(new Promise((resolve) => (finish = resolve)), { cancel }),
  );

  const view = render(<Harness importer="WWMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "page.mod.dialog.uncompressed-textures.confirm" }),
  );
  await screen.findByRole("status");
  view.unmount();
  await act(async () => finish({ files: [] }));

  expect(cancel).toHaveBeenCalledTimes(1);
  expect(xxmi.StartGame).not.toHaveBeenCalled();
});

it.each([[[]], [null]])(
  "does not launch when the page unmounts while textures are scanned (%j)",
  async (found) => {
    const scanning = Promise.withResolvers<never[] | null>();
    const cancel = vi.fn();
    xxmi.LaunchUncompressedTextures.mockReturnValue(Object.assign(scanning.promise, { cancel }));
    xxmi.StartGame.mockResolvedValue(undefined);

    const view = render(<Harness importer="WWMI" />);
    fireEvent.click(screen.getByRole("button", { name: "play" }));
    await waitFor(() => expect(xxmi.LaunchUncompressedTextures).toHaveBeenCalledWith("WWMI"));
    view.unmount();
    await act(async () => {
      if (found) scanning.resolve(found);
      else scanning.reject(new Error("cancelled"));
      await scanning.promise.catch(() => null);
    });

    expect(cancel).toHaveBeenCalledTimes(1);
    expect(xxmi.StartGame).not.toHaveBeenCalled();
  },
);

it("cancels every overlapping texture scan when the page unmounts", async () => {
  const scans = [Promise.withResolvers<never[]>(), Promise.withResolvers<never[]>()];
  const cancels = [vi.fn(), vi.fn()];
  scans.forEach((scan, index) =>
    xxmi.LaunchUncompressedTextures.mockReturnValueOnce(
      Object.assign(scan.promise, { cancel: cancels[index] }),
    ),
  );
  xxmi.StartGame.mockResolvedValue(undefined);

  const view = render(<Harness importer="GIMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await waitFor(() => expect(xxmi.LaunchUncompressedTextures).toHaveBeenCalledTimes(2));
  view.unmount();
  await act(async () => {
    scans.forEach((scan) => scan.resolve([]));
    await Promise.all(scans.map((scan) => scan.promise));
  });

  cancels.forEach((cancel) => expect(cancel).toHaveBeenCalledTimes(1));
  expect(xxmi.StartGame).not.toHaveBeenCalled();
});

const pendingUpdates = [
  { importer: "GIMI", package: "importer:GIMI", installed: "1.2.3", latestVersion: "1.3.0" },
  { importer: "GIMI", package: "xxmi-libs", installed: "1.7.5", latestVersion: "1.7.6" },
];

it("asks before launching with pending updates and launches after installing them", async () => {
  xxmi.LaunchUpdates.mockResolvedValue(pendingUpdates);
  xxmi.InstallUpdates.mockResolvedValue(["importer:GIMI", "xxmi-libs"]);
  xxmi.StartGame.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));

  expect(await screen.findByText(/importer:GIMI: 1\.2\.3.*1\.3\.0/)).toBeTruthy();
  expect(screen.getByText(/xxmi-libs: 1\.7\.5.*1\.7\.6/)).toBeTruthy();
  expect(xxmi.StartGame).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.launchUpdateConfirm" }),
  );

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledWith("GIMI"));
  expect(xxmi.InstallUpdates).toHaveBeenCalledWith("GIMI", ["importer:GIMI", "xxmi-libs"]);
});

it("does not launch or update when the update dialog is cancelled", async () => {
  xxmi.LaunchUpdates.mockResolvedValue(pendingUpdates);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByText(/importer:GIMI/);
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));

  await waitFor(() => expect(screen.queryByText(/importer:GIMI/)).toBeNull());
  expect(xxmi.InstallUpdates).not.toHaveBeenCalled();
  expect(xxmi.StartGame).not.toHaveBeenCalled();
});

it("launches with the current files when the confirmed update fails", async () => {
  xxmi.LaunchUpdates.mockResolvedValue(pendingUpdates);
  xxmi.InstallUpdates.mockRejectedValue(new Error("download failed"));
  xxmi.StartGame.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByText(/importer:GIMI/);
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.launchUpdateConfirm" }),
  );

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledWith("GIMI"));
  expect(toastWarning).toHaveBeenCalledWith("page.setting.xxmi.builtin.launchUpdateFailed", {
    description: "download failed",
  });
});

it("launches when the update check itself fails", async () => {
  xxmi.LaunchUpdates.mockRejectedValue(new Error("offline"));
  xxmi.StartGame.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));

  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledWith("GIMI"));
});

it("closes the launch dialog before StartGame finishes", async () => {
  const pending = Promise.withResolvers<void>();
  xxmi.StartGame.mockRejectedValueOnce(new Error("GIMI_DCR_ENABLED")).mockReturnValueOnce(
    pending.promise,
  );
  xxmi.ClearLaunchBlockers.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.gimi-dcr.confirm" }));
  await waitFor(() => {
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
  expect(xxmi.ClearLaunchBlockers).toHaveBeenCalledWith("GIMI");
  expect(xxmi.StartGame).toHaveBeenCalledTimes(2);

  pending.resolve();
  await act(async () => {
    await pending.promise;
  });
});

it("surfaces a failed launch instead of reopening the dialog after clearing", async () => {
  xxmi.StartGame.mockRejectedValue(new Error("GIMI_DCR_ENABLED"));
  xxmi.ClearLaunchBlockers.mockResolvedValue(undefined);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.gimi-dcr.confirm" }));
  await waitFor(() => {
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
  expect(xxmi.StartGame).toHaveBeenCalledTimes(2);
  expect(toastError).toHaveBeenCalledTimes(1);
});

it("does not launch after the dialog closes while blockers are being cleared", async () => {
  const clearing = Promise.withResolvers<void>();
  xxmi.StartGame.mockRejectedValueOnce(new Error("GIMI_DCR_ENABLED"));
  xxmi.ClearLaunchBlockers.mockReturnValue(clearing.promise);

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(screen.getByRole("button", { name: "page.mod.dialog.gimi-dcr.confirm" }));
  await waitFor(() => expect(xxmi.ClearLaunchBlockers).toHaveBeenCalledWith("GIMI"));

  fireEvent.keyDown(document, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  await act(async () => {
    clearing.resolve();
    await clearing.promise;
  });
  expect(xxmi.StartGame).toHaveBeenCalledTimes(1);
});

it("offers the effect packages a preset lacks once, and launches either way", async () => {
  const effects = { packages: [{ id: "pack", name: "Pack", description: "" }], unknown: [] };
  xxmi.LaunchPresetEffects.mockResolvedValue(effects);
  xxmi.StartGame.mockResolvedValue(undefined);
  reshade.InstallEffectPackages.mockRejectedValue(new Error("download failed"));
  const install = "page.setting.xxmi.builtin.reshade.presetEffectsInstallAndLaunch";

  render(<Harness />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByText("Pack")).toBeTruthy();
  expect(xxmi.StartGame).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: install }));
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(1));
  expect(reshade.InstallEffectPackages).toHaveBeenCalledWith(["pack"]);

  fireEvent.click(screen.getByRole("button", { name: "play" }));
  fireEvent.click(
    await screen.findByRole("button", {
      name: "page.setting.xxmi.builtin.reshade.presetEffectsSkipLaunch",
    }),
  );
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(2));

  fireEvent.click(screen.getByRole("button", { name: "play" }));
  await waitFor(() => expect(xxmi.StartGame).toHaveBeenCalledTimes(3));
  expect(screen.queryByRole("button", { name: install })).toBeNull();
});
