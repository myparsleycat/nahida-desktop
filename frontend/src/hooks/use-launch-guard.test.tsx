// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  StartGame: vi.fn(),
  ClearLaunchBlockers: vi.fn(),
}));

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

import { toast } from "sonner";

import { launchDialog, useLaunchGuard } from "./use-launch-guard";

const toastError = vi.mocked(toast.error);

afterEach(() => {
  cleanup();
  xxmi.StartGame.mockReset();
  xxmi.ClearLaunchBlockers.mockReset();
  toastError.mockClear();
});

it("picks one launch dialog for the blocker codes", () => {
  expect(launchDialog("GIMI_DCR_ENABLED")).toBe("gimi-dcr");
  expect(launchDialog("NVIDIA_SMOOTH_MOTION_ENABLED")).toBe("smooth-motion");
  expect(launchDialog("GIMI_DCR_ENABLED\nNVIDIA_SMOOTH_MOTION_ENABLED")).toBe("launch-blockers");
  expect(launchDialog("XXMI is not configured")).toBeNull();
});

function Harness() {
  const { startImporter, launchGuardDialog } = useLaunchGuard();
  return (
    <div>
      <button type="button" onClick={() => void startImporter("GIMI")}>
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
