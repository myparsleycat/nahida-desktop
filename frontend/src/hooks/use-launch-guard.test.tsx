// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  StartGame: vi.fn(),
  StartGameWithCompatibility: vi.fn(),
  ClearLaunchBlockers: vi.fn(),
  GetImporterConfig: vi.fn(),
  SaveImporterConfig: vi.fn(),
  DetectGameFolders: vi.fn(),
  ValidateGameFolder: vi.fn(),
  RepairRuntime: vi.fn(),
}));

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), warning: vi.fn() } }));

import { toast } from "sonner";

import { launchDialog, useLaunchGuard } from "./use-launch-guard";

const toastError = vi.mocked(toast.error);

afterEach(() => {
  cleanup();
  xxmi.StartGame.mockReset();
  xxmi.StartGameWithCompatibility.mockReset();
  xxmi.ClearLaunchBlockers.mockReset();
  xxmi.GetImporterConfig.mockReset();
  xxmi.SaveImporterConfig.mockReset();
  xxmi.DetectGameFolders.mockReset();
  xxmi.ValidateGameFolder.mockReset();
  xxmi.RepairRuntime.mockReset();
  toastError.mockClear();
});

it("picks one launch dialog for the blocker codes", () => {
  expect(launchDialog("GIMI_DCR_ENABLED")).toBe("gimi-dcr");
  expect(launchDialog("NVIDIA_SMOOTH_MOTION_ENABLED")).toBe("smooth-motion");
  expect(launchDialog("GIMI_DCR_ENABLED\nNVIDIA_SMOOTH_MOTION_ENABLED")).toBe("launch-blockers");
  expect(launchDialog("WWMI_WOUNDED_FX_DECISION_REQUIRED")).toBe("wwmi-wounded");
  expect(launchDialog("XXMI_GAME_FOLDER_NOT_CONFIGURED")).toBe("game-folder");
  expect(launchDialog("XXMI_RUNTIME_CORRUPTED")).toBe("runtime-repair");
  expect(launchDialog("XXMI_LIBS_TOO_OLD")).toBe("old-libs");
  expect(launchDialog("XXMI is not configured")).toBeNull();
});

it("launches EFMI with old libraries only after confirmation", async () => {
  xxmi.StartGame.mockRejectedValueOnce(new Error("XXMI_LIBS_TOO_OLD"));
  xxmi.StartGameWithCompatibility.mockResolvedValueOnce(undefined);

  render(<Harness importer="EFMI" />);
  fireEvent.click(screen.getByRole("button", { name: "play" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(xxmi.StartGameWithCompatibility).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.launchAnyway" }));

  await waitFor(() => expect(xxmi.StartGameWithCompatibility).toHaveBeenCalledWith("EFMI", true));
  expect(xxmi.StartGame).toHaveBeenCalledTimes(1);
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
