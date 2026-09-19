// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  EnsureSession: vi.fn<() => Promise<void>>(),
  ToggleModLike: vi.fn(),
}));
vi.mock("@bindings/gamebanana", () => ({ GameBanana: backend }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { error: vi.fn() } }));
vi.mock("@renderer/hooks/use-gamebanana-data", () => ({
  useGameBananaModPosts: () => ({ data: undefined, isLoading: false, error: null }),
}));

import { toast } from "sonner";

import type { ModOverviewQuery } from "../-types";

import { ModDetailPanel } from "./mod-detail-panel";

const t = ((key: string) => key) as unknown as Parameters<typeof ModDetailPanel>[0]["t"];

function buildQuery(likeAccess: { Like_Add?: boolean; Like_Trash?: boolean }) {
  return {
    data: {
      profile: {
        _idRow: 10,
        _sName: "Mod",
        _nLikeCount: 3,
        _aCategory: { _sName: "Category" },
        _aSubmitter: { _sName: "author" },
        _aGame: { _sName: "Game" },
        _aFiles: [],
      },
      config: { _aAccess: likeAccess },
    },
    isLoading: false,
    error: null,
    refetch: vi.fn(async () => ({ isError: false })),
  };
}

function renderPanel(query: ReturnType<typeof buildQuery>, isSignedIn: boolean) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return {
    invalidateQueries,
    ...render(
      <ModDetailPanel
        t={t}
        language="en"
        selection={{ id: 10, modelName: "Mod" }}
        modOverviewQuery={query as unknown as ModOverviewQuery}
        isSignedIn={isSignedIn}
      />,
      { wrapper },
    ),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  backend.EnsureSession.mockResolvedValue();
  backend.ToggleModLike.mockResolvedValue({ liked: true });
});
afterEach(cleanup);

describe("ModDetailPanel like without a session", () => {
  it("signs in and toggles the like once when the anonymous button is clicked", async () => {
    const query = buildQuery({});
    const { invalidateQueries } = renderPanel(query, false);

    const likeButton = await screen.findByTitle("page.gamebanana.like_sign_in_required");
    expect(likeButton.hasAttribute("disabled")).toBe(false);

    fireEvent.click(likeButton);

    await waitFor(() => expect(backend.ToggleModLike).toHaveBeenCalledTimes(1));
    expect(backend.EnsureSession).toHaveBeenCalledTimes(1);
    expect(query.refetch).toHaveBeenCalled();
    await waitFor(() =>
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["gamebanana"] }),
    );
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps the like untouched when the sign-in window is cancelled", async () => {
    backend.EnsureSession.mockRejectedValue(new Error("GAMEBANANA_LOGIN_CANCELLED"));
    const query = buildQuery({});
    renderPanel(query, false);

    fireEvent.click(await screen.findByTitle("page.gamebanana.like_sign_in_required"));

    await waitFor(() => expect(backend.EnsureSession).toHaveBeenCalledTimes(1));
    expect(backend.ToggleModLike).not.toHaveBeenCalled();
    expect(toast.error).toHaveBeenCalledWith("page.gamebanana.auth.cancelled_title", {
      description: "page.gamebanana.auth.cancelled_description",
    });
  });

  it("keeps the button disabled for a signed-in member without like permission", async () => {
    renderPanel(buildQuery({}), true);

    const likeButton = await screen.findByTitle("page.gamebanana.like_unavailable");
    expect(likeButton.hasAttribute("disabled")).toBe(true);

    fireEvent.click(likeButton);
    expect(backend.EnsureSession).not.toHaveBeenCalled();
    expect(backend.ToggleModLike).not.toHaveBeenCalled();
  });
});
