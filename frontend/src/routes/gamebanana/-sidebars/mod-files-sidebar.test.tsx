// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const gameBanana = vi.hoisted(() => ({
  EnsureSession: vi.fn<() => Promise<void>>(),
}));
const mod = vi.hoisted(() => ({
  DownloadGameBananaFile: vi.fn(),
}));
vi.mock("@bindings/gamebanana", () => ({ GameBanana: gameBanana }));
vi.mock("@bindings/mod", () => ({ Mod: mod }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { error: vi.fn() } }));

import { toast } from "sonner";

import type { ModOverviewQuery } from "../-types";

import { ModFilesSidebar } from "./mod-files-sidebar";

const t = ((key: string) => key) as unknown as Parameters<typeof ModFilesSidebar>[0]["t"];

function buildQuery() {
  return {
    data: {
      profile: {
        _idRow: 10,
        _nLikeCount: 1,
        _nPostCount: 1,
        _nViewCount: 1,
        _nDownloadCount: 1,
        _aFiles: [
          { _idRow: 20, _sFile: "mod.zip", _nDownloadCount: 5, _tsDateAdded: 1_700_000_000 },
        ],
      },
      config: {},
    },
    isLoading: false,
    error: null,
    refetch: vi.fn(async () => ({ isError: false })),
  };
}

function renderSidebar(query: ReturnType<typeof buildQuery>) {
  return render(
    <ModFilesSidebar t={t} language="en" modOverviewQuery={query as unknown as ModOverviewQuery} />,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  gameBanana.EnsureSession.mockResolvedValue();
  mod.DownloadGameBananaFile.mockResolvedValue("started");
});
afterEach(cleanup);

describe("ModFilesSidebar download authentication", () => {
  it("signs in and retries the download once when the session is required", async () => {
    mod.DownloadGameBananaFile.mockRejectedValueOnce(new Error("GAMEBANANA_AUTH_REQUIRED"));
    renderSidebar(buildQuery());

    fireEvent.click(await screen.findByText("mod.zip"));

    await waitFor(() => expect(mod.DownloadGameBananaFile).toHaveBeenCalledTimes(2));
    expect(gameBanana.EnsureSession).toHaveBeenCalledTimes(1);
    expect(mod.DownloadGameBananaFile).toHaveBeenLastCalledWith({
      itemId: 10,
      fileId: 20,
      modelName: "Mod",
    });
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("stops after a cancelled sign-in and reports it", async () => {
    mod.DownloadGameBananaFile.mockRejectedValue(new Error("GAMEBANANA_AUTH_REQUIRED"));
    gameBanana.EnsureSession.mockRejectedValue(new Error("GAMEBANANA_LOGIN_CANCELLED"));
    renderSidebar(buildQuery());

    fireEvent.click(await screen.findByText("mod.zip"));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("page.gamebanana.auth.cancelled_title", {
        description: "page.gamebanana.auth.cancelled_description",
      }),
    );
    expect(mod.DownloadGameBananaFile).toHaveBeenCalledTimes(1);
    expect(toast.error).not.toHaveBeenCalledWith(
      "page.gamebanana.download_failed",
      expect.anything(),
    );
  });

  it("keeps the plain failure toast for other download errors", async () => {
    mod.DownloadGameBananaFile.mockRejectedValue(new Error("GAMEBANANA_DOWNLOAD_HEAD_FAILED:404"));
    renderSidebar(buildQuery());

    fireEvent.click(await screen.findByText("mod.zip"));

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("page.gamebanana.download_failed", {
        description: "GAMEBANANA_DOWNLOAD_HEAD_FAILED:404",
      }),
    );
    expect(gameBanana.EnsureSession).not.toHaveBeenCalled();
    expect(mod.DownloadGameBananaFile).toHaveBeenCalledTimes(1);
  });
});
