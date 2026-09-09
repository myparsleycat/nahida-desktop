// @vitest-environment jsdom

import { act, cleanup, render, screen } from "@testing-library/react";
import { Suspense, type ComponentType } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const backend = vi.hoisted(() => ({
  EnsureSession: vi.fn<() => Promise<void>>(),
  Logout: vi.fn<() => Promise<void>>(),
  SetManualRMCToken: vi.fn(),
}));
const navigate = vi.hoisted(() => vi.fn());
vi.mock("@bindings/gamebanana", () => ({ GameBanana: backend }));
vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
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
}));
vi.mock("./-components/gamebanana-toolbar", () => ({ GameBananaToolbar: () => null }));
vi.mock("./-panels/game-home-panel", () => ({
  GameHomePanel: () => <div>authenticated-content</div>,
}));
vi.mock("./-panels/category-panel", () => ({ CategoryPanel: () => null }));
vi.mock("./-panels/mod-detail-panel", () => ({ ModDetailPanel: () => null }));
vi.mock("./-sidebars/category-sidebar", () => ({ CategorySidebar: () => null }));
vi.mock("./-sidebars/mod-files-sidebar", () => ({ ModFilesSidebar: () => null }));

import { Route } from "./index";

const ROUTE_LOAD_TIMEOUT_MS = 5_000;

async function renderRoute() {
  const Component = Route.options.component;
  if (!Component) throw new Error("GameBanana route has no component");
  let view: ReturnType<typeof render> | undefined;
  await act(async () => {
    view = render(
      <Suspense fallback={null}>
        <Component />
      </Suspense>,
    );
  });
  if (!view) throw new Error("Route failed to mount");
  return view;
}

beforeEach(() => {
  vi.clearAllMocks();
  backend.EnsureSession.mockResolvedValue();
});
afterEach(cleanup);

describe("GameBanana route authentication lifecycle", () => {
  it("revalidates when returning without logging out on unmount", async () => {
    const first = await renderRoute();
    await screen.findByText("authenticated-content", undefined, {
      timeout: ROUTE_LOAD_TIMEOUT_MS,
    });
    first.unmount();
    expect(backend.Logout).not.toHaveBeenCalled();
    await renderRoute();
    await screen.findByText("authenticated-content", undefined, {
      timeout: ROUTE_LOAD_TIMEOUT_MS,
    });
    expect(backend.EnsureSession).toHaveBeenCalledTimes(2);
    expect(backend.Logout).not.toHaveBeenCalled();
  });

  it.each([
    ["GAMEBANANA_LOGIN_INIT_FAILED", "init_failed_title"],
    ["GAMEBANANA_AUTH_CHECK_FAILED", "check_failed_title"],
  ])("shows retry guidance for %s", async (code, title) => {
    backend.EnsureSession.mockRejectedValue(new Error(code));
    await renderRoute();
    await screen.findByText(`page.gamebanana.auth.${title}`);
    expect(screen.queryByText("authenticated-content")).toBeNull();
    expect(backend.Logout).not.toHaveBeenCalled();
  });
});
