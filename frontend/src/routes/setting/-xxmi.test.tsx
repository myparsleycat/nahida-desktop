// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  launcherMode: undefined as string | undefined,
  xxmiData: {
    mode: "external",
    xxmiPath: "C:\XXMI Launcher",
    dllVersion: "v1.7.6",
    enabledImporters: [{ key: "GIMI", installedVersion: "1.2.3" }],
  },
  overview: {
    configured: true,
    root: "C:\\XXMI",
    libsCache: [],
    legacyRuntimes: [],
    fpsVersions: [],
    cacheIssues: ["legacy 3DMigoto: missing source.json"],
    importers: [
      {
        key: "GIMI",
        mode: "xxmi",
        running: true,
        updateAvailable: true,
        packageInfo: { deployed_version: "1.2.3" },
      },
    ],
  },
  updates: [] as Array<{
    importer: string;
    package: string;
    installed: string;
    latestVersion: string;
    pinned: boolean;
    available: boolean;
  }>,
}));

vi.mock("@bindings/xxmi", () => ({ XXMI: {} }));
vi.mock("@bindings/platform", () => ({ Dialog: {} }));
vi.mock("@renderer/hooks/use-launch-guard", () => ({
  useLaunchGuard: () => ({ startImporter: vi.fn(), launchGuardDialog: null }),
}));
vi.mock("@renderer/hooks/use-settings", () => ({
  useSettings: () => ({
    settings: { autoUpdate: false, includePrereleases: false },
    update: vi.fn(),
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({
    data:
      queryKey[0] === "xxmi:overview"
        ? { ...state.overview, launcherMode: state.launcherMode }
        : queryKey[0] === "xxmi:getXXMIData"
          ? state.xxmiData
          : state.updates,
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (options: object) => ({ options }),
  lazyRouteComponent: (component: unknown) => component,
  useLocation: () => ({ pathname: "/setting/xxmi" }),
  useNavigate: () => vi.fn(),
  Outlet: () => null,
}));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { XXMIDashboard } from "./xxmi";

afterEach(() => {
  cleanup();
  state.updates = [];
  state.launcherMode = undefined;
});

it("shows the external launcher settings instead of the built-in runtime in external mode", () => {
  state.launcherMode = "external";

  render(<XXMIDashboard />);

  expect(screen.getByDisplayValue("C:\XXMI Launcher")).toBeTruthy();
  expect(screen.getByText("page.setting.xxmi.activeImporter")).toBeTruthy();
  expect(screen.queryByText("page.setting.xxmi.builtin.root")).toBeNull();
  expect(
    screen.getByRole("button", { name: "page.setting.xxmi.launcherMode.external" }),
  ).toHaveProperty("disabled", true);
});

it("keeps the package controls visible when a cache is damaged and disables a running importer", () => {
  render(<XXMIDashboard />);

  expect(screen.getByRole("alert").textContent).toContain("missing source.json");
  expect(screen.getByText(/GIMI.*updateAvailable.*running/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.launch" })).toHaveProperty(
    "disabled",
    true,
  );
});

it("shows a shared library update when the first importer pins it and another follows latest", () => {
  state.updates = [
    {
      importer: "GIMI",
      package: "xxmi-libs",
      installed: "1.7.5",
      latestVersion: "1.7.6",
      pinned: true,
      available: true,
    },
    {
      importer: "WWMI",
      package: "xxmi-libs",
      installed: "1.7.5",
      latestVersion: "1.7.6",
      pinned: false,
      available: true,
    },
  ];

  render(<XXMIDashboard />);

  expect(screen.getByText(/xxmi-libs: 1\.7\.5.*1\.7\.6/)).toBeTruthy();
  expect(
    screen.getAllByRole("button", { name: "page.setting.xxmi.builtin.skipVersion" }),
  ).toHaveLength(1);
});
