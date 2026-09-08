// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({
  select: vi.fn().mockResolvedValue(undefined),
  logout: vi.fn(),
  navigate: vi.fn(),
  setDownloadMode: vi.fn(),
  reset: vi.fn(),
}));
vi.mock("@bindings/mod", () => ({ Mod: { SelectModManagerPath: calls.select } }));
vi.mock("@bindings/gamebanana", () => ({ GameBanana: { Logout: calls.logout } }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => calls.navigate }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("@renderer/hooks/use-settings", () => ({ useSetting: () => ({ data: true }) }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { error: vi.fn() } }));
vi.mock("@renderer/lib/settings", () => ({ setSetting: vi.fn() }));
vi.mock("@renderer/store/mod", () => {
  const state = {
    downloadMode: {
      downloadId: "download",
      downloadSource: "gamebanana",
      suggestedName: "Example",
    },
    selectedGroup: { path: "C:\\Mods", name: "Mods" },
    setDownloadMode: calls.setDownloadMode,
    resetUserSelectedDuringDownload: calls.reset,
  };
  return { useModStore: (select: (value: typeof state) => unknown) => select(state) };
});

import { DownloadConfirmationOverlay } from "./download-confirmation-overlay";

afterEach(cleanup);

it("returns to GameBanana after accepting the download without logging out", async () => {
  render(<DownloadConfirmationOverlay />);
  fireEvent.click(screen.getByRole("button", { name: "g.select" }));
  await waitFor(() => expect(calls.navigate).toHaveBeenCalledWith({ to: "/gamebanana" }));
  expect(calls.select).toHaveBeenCalledWith("download", "C:\\Mods", "Example");
  expect(calls.setDownloadMode).toHaveBeenCalledWith(null);
  expect(calls.logout).not.toHaveBeenCalled();
});
