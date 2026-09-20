// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Suspense, type ComponentType, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  EnsureSession: vi.fn<() => Promise<void>>(),
  Logout: vi.fn<() => Promise<void>>(),
  SetManualRMCToken: vi.fn(),
}));
const session = vi.hoisted(() => ({
  status: undefined as { data?: { authenticated: boolean; username?: string } } | undefined,
}));
const navigate = vi.hoisted(() => vi.fn());
vi.mock("@bindings/gamebanana", () => ({ GameBanana: backend }));
vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  createFileRoute: () => (options: { component: ComponentType }) => ({
    options,
    useSearch: () => ({}),
    useNavigate: () => navigate,
  }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }),
}));
vi.mock("@renderer/store/mod", () => ({ modStore: { getState: () => ({ selectedGame: "" }) } }));
vi.mock("@renderer/hooks/use-mod-data", () => ({ useGames: () => ({ data: [] }) }));
vi.mock("@renderer/hooks/use-gamebanana-data", () => ({
  useGameBananaGames: () => ({ data: {} }),
  useGameBananaGameOverview: () => ({}),
  useGameBananaGameSubfeed: () => ({}),
  useGameBananaModCategoryOverview: () => ({}),
  useGameBananaModOverview: () => ({}),
  useGameBananaSessionStatus: () => session.status,
}));
vi.mock("./-panels/game-home-panel", () => ({
  GameHomePanel: () => <div>gamebanana-content</div>,
}));
vi.mock("./-panels/category-panel", () => ({ CategoryPanel: () => null }));
vi.mock("./-panels/mod-detail-panel", () => ({ ModDetailPanel: () => null }));
vi.mock("./-sidebars/category-sidebar", () => ({ CategorySidebar: () => null }));
vi.mock("./-sidebars/mod-files-sidebar", () => ({ ModFilesSidebar: () => null }));

import { toast } from "sonner";

import { Route } from "./index";

const ROUTE_LOAD_TIMEOUT_MS = 5_000;

async function renderRoute() {
  const Component = Route.options.component;
  if (!Component) throw new Error("GameBanana route has no component");
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidateQueries = vi.spyOn(queryClient, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  let view: ReturnType<typeof render> | undefined;
  await act(async () => {
    view = render(
      <Suspense fallback={null}>
        <Component />
      </Suspense>,
      { wrapper },
    );
  });
  if (!view) throw new Error("Route failed to mount");
  return { view, invalidateQueries };
}

async function renderLoadedRoute() {
  const rendered = await renderRoute();
  await screen.findByText("gamebanana-content", undefined, {
    timeout: ROUTE_LOAD_TIMEOUT_MS,
  });
  return rendered;
}

beforeEach(() => {
  vi.clearAllMocks();
  session.status = { data: { authenticated: false } };
  backend.EnsureSession.mockResolvedValue();
  backend.Logout.mockResolvedValue();
});
afterEach(cleanup);

describe("GameBanana route session handling", () => {
  it("renders browsing content without opening the login window", async () => {
    await renderLoadedRoute();

    expect(backend.EnsureSession).not.toHaveBeenCalled();
    expect(screen.getByText("page.gamebanana.auth.sign_in")).toBeTruthy();
  });

  it("shows the signed-in account instead of the sign-in action", async () => {
    session.status = { data: { authenticated: true, username: "member" } };
    await renderLoadedRoute();

    expect(screen.getByText("member")).toBeTruthy();
    expect(screen.queryByText("page.gamebanana.auth.sign_in")).toBeNull();
    expect(screen.getByText("page.gamebanana.logout")).toBeTruthy();
    expect(backend.EnsureSession).not.toHaveBeenCalled();
  });

  it("signs in from the toolbar and refreshes GameBanana data", async () => {
    const { invalidateQueries } = await renderLoadedRoute();

    await act(async () => {
      fireEvent.click(screen.getByText("page.gamebanana.auth.sign_in"));
    });

    await waitFor(() => expect(backend.EnsureSession).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["gamebanana"] }),
    );
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("reports a cancelled sign-in as a toast and keeps the page", async () => {
    backend.EnsureSession.mockRejectedValue(new Error("GAMEBANANA_LOGIN_CANCELLED"));
    await renderLoadedRoute();

    await act(async () => {
      fireEvent.click(screen.getByText("page.gamebanana.auth.sign_in"));
    });

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("page.gamebanana.auth.cancelled_title", {
        description: "page.gamebanana.auth.cancelled_description",
      }),
    );
    expect(screen.getByText("gamebanana-content")).toBeTruthy();
  });

  it("signs out from the toolbar and refreshes GameBanana data", async () => {
    session.status = { data: { authenticated: true, username: "member" } };
    const { invalidateQueries } = await renderLoadedRoute();

    await act(async () => {
      fireEvent.click(screen.getByText("page.gamebanana.logout"));
    });

    await waitFor(() => expect(backend.Logout).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ["gamebanana"] }),
    );
    expect(toast.success).toHaveBeenCalledWith("page.gamebanana.auth.signed_out");
  });
});
