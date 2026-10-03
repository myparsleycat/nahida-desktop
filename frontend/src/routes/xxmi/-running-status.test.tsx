// @vitest-environment jsdom

import { XXMI } from "@bindings/xxmi";
import { LauncherMode, RuntimeXXMI, type Overview } from "@bindings/xxmi/models";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { QueryClient, QueryClientProvider, focusManager } from "@tanstack/react-query";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { XXMILayout } from "./route";

const events = vi.hoisted(() => ({
  listeners: new Map<string, (event: { data: unknown }) => void>(),
  i18n: { t: (key: string) => key, changeLanguage: vi.fn() },
}));

vi.mock("@bindings/auth", () => ({ Auth: {} }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, listener: (event: { data: unknown }) => void) => {
      events.listeners.set(name, listener);
      return () => events.listeners.delete(name);
    },
  },
}));
vi.mock("@bindings/xxmi", () => ({
  XXMI: { GetOverview: vi.fn(), CheckUpdates: vi.fn() },
}));
vi.mock("@renderer/hooks/use-launch-guard", () => ({
  useLaunchGuard: () => ({ startImporter: vi.fn(), launchGuardDialog: null }),
}));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (options: object) => ({ options }),
  useLocation: () => ({ pathname: "/xxmi/GIMI" }),
  useNavigate: () => vi.fn(),
  Outlet: () => null,
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: events.i18n }),
}));

const runningLabel = "page.setting.xxmi.builtin.running";
const launchLabel = "page.setting.xxmi.builtin.launch";
let client: QueryClient;
let overview: Overview;

beforeEach(() => {
  vi.useFakeTimers();
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  overview = {
    launcherMode: LauncherMode.LauncherBuiltin,
    configured: true,
    root: "C:\\XXMI",
    sharedLibsVersion: "",
    importers: [
      {
        key: "GIMI",
        mode: RuntimeXXMI,
        importerFolder: "C:\\XXMI\\GIMI",
        gameFolder: "C:\\Games\\Genshin Impact",
        running: true,
        updateAvailable: false,
        installedVersion: "1.2.3",
        customDll: false,
        packageInfo: {
          latest_version: "1.2.3",
          skipped_version: "",
          deployed_version: "1.2.3",
          update_check_time: 0,
          latest_release_notes: "",
          deployed_release_notes: "",
        },
      },
    ],
    libsCache: [],
    legacyRuntimes: [],
    fpsVersions: [],
  };
  vi.mocked(XXMI.CheckUpdates).mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  client.clear();
  focusManager.setFocused(undefined);
  vi.useRealTimers();
  vi.resetAllMocks();
  events.listeners.clear();
});

function GlobalEvents() {
  useGlobalEvents();
  return null;
}

function openXXMI(showPage = true) {
  client.setQueryData(["xxmi:overview"], overview);
  return render(
    <QueryClientProvider client={client}>
      <GlobalEvents />
      {showPage && <XXMILayout />}
    </QueryClientProvider>,
  );
}

// Query observers are notified on a timer, so the fake clock has to move before the page rerenders.
async function emitRunning(running: Record<string, boolean>) {
  act(() => events.listeners.get("xxmi:running-changed")?.({ data: running }));
  await act(() => vi.advanceTimersByTimeAsync(0));
}

it("clears the running indicator and enables launch after the game exits while unfocused", async () => {
  focusManager.setFocused(false);
  const view = openXXMI();
  expect(screen.getByText(runningLabel)).toBeTruthy();
  expect(screen.getByRole("button", { name: launchLabel })).toHaveProperty("disabled", true);

  await emitRunning({ GIMI: false });

  expect(screen.queryByText(runningLabel)).toBeNull();
  expect(screen.getByRole("button", { name: launchLabel })).toHaveProperty("disabled", false);

  await act(() => vi.advanceTimersByTimeAsync(3000));
  expect(XXMI.GetOverview).not.toHaveBeenCalled();
  view.unmount();
  expect(events.listeners.has("xxmi:running-changed")).toBe(false);
});

it("detects a game started outside the page and continues updating after it exits", async () => {
  overview = {
    ...overview,
    importers: overview.importers?.map((importer) => ({ ...importer, running: false })) ?? [],
  };
  openXXMI();
  expect(screen.queryByText(runningLabel)).toBeNull();

  await emitRunning({ GIMI: true });
  expect(screen.getByText(runningLabel)).toBeTruthy();
  expect(screen.getByRole("button", { name: launchLabel })).toHaveProperty("disabled", true);

  await emitRunning({ GIMI: false });
  expect(screen.queryByText(runningLabel)).toBeNull();
  expect(screen.getByRole("button", { name: launchLabel })).toHaveProperty("disabled", false);
  expect(XXMI.GetOverview).not.toHaveBeenCalled();
});

it("updates cached status outside XXMI so the page opens with the current status", async () => {
  const view = openXXMI(false);

  await emitRunning({ GIMI: false });
  expect(client.getQueryData<Overview>(["xxmi:overview"])?.importers?.[0]?.running).toBe(false);

  view.rerender(
    <QueryClientProvider client={client}>
      <GlobalEvents />
      <XXMILayout />
    </QueryClientProvider>,
  );
  expect(screen.queryByText(runningLabel)).toBeNull();
  expect(screen.getByRole("button", { name: launchLabel })).toHaveProperty("disabled", false);
  expect(XXMI.GetOverview).not.toHaveBeenCalled();
});

it("refetches the overview when the watched importers differ from the cached ones", async () => {
  openXXMI();
  vi.mocked(XXMI.GetOverview).mockResolvedValue({
    ...overview,
    importers: overview.importers?.map((importer) => ({ ...importer, running: false })) ?? [],
  });

  await emitRunning({ GIMI: false, SRMI: false });
  await act(() => vi.advanceTimersByTimeAsync(10));

  expect(XXMI.GetOverview).toHaveBeenCalledOnce();
  expect(screen.queryByText(runningLabel)).toBeNull();
});

it("does not poll the overview in external launcher mode", async () => {
  overview.launcherMode = LauncherMode.LauncherExternal;
  openXXMI();

  await act(() => vi.advanceTimersByTimeAsync(3000));
  expect(XXMI.GetOverview).not.toHaveBeenCalled();
});

it("does not poll when no importers are installed", async () => {
  overview.configured = false;
  overview.importers = [];
  openXXMI();

  await act(() => vi.advanceTimersByTimeAsync(3000));
  expect(XXMI.GetOverview).not.toHaveBeenCalled();
});
