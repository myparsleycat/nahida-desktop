// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  launcherMode: undefined as string | undefined,
  xxmiData: {
    mode: "external",
    xxmiPath: "C:\XXMI Launcher",
    dllVersion: "v1.7.6",
    enabledImporters: [{ key: "GIMI", installedVersion: "1.2.3" }],
    disabledImporters: [{ key: "SRMI", installedVersion: "4.5.6" }],
  },
  overview: {
    configured: true,
    root: "C:\\XXMI",
    libsCache: [] as Array<{ version: string; referenced: boolean; inUse: boolean }>,
    legacyRuntimes: [],
    fpsVersions: [],
    cacheIssues: ["legacy 3DMigoto: missing source.json"],
    importers: [
      {
        key: "GIMI",
        mode: "xxmi",
        running: true,
        updateAvailable: true,
        customDll: true,
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
    shared?: boolean;
  }>,
}));

const xxmi = vi.hoisted(() => ({ SetSharedLibsVersion: vi.fn() }));

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("@bindings/platform", () => ({ Dialog: {} }));
vi.mock("@renderer/hooks/use-launch-guard", () => ({
  useLaunchGuard: () => ({ startImporter: vi.fn(), launchGuardDialog: null }),
}));
vi.mock("@renderer/hooks/use-settings", () => ({
  useSettings: () => ({
    settings: { autoUpdateMode: "off", includePrereleases: false },
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
          : queryKey[0] === "xxmi:libs-releases"
            ? [{ version: "1.7.5" }, { version: "1.7.6" }]
            : state.updates,
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (options: object) => ({ options }),
  lazyRouteComponent: (component: unknown) => component,
  useLocation: () => ({ pathname: "/xxmi" }),
  useNavigate: () => vi.fn(),
  Outlet: () => null,
}));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { XXMIImporterList } from "@renderer/components/xxmi/xxmi-importer-list";

import { XXMIDashboard } from "./index";
import { XXMILayout } from "./route";

afterEach(() => {
  cleanup();
  state.updates = [];
  state.launcherMode = undefined;
  state.overview.importers[0].running = true;
  state.overview.libsCache = [];
});

it("marks only the selected libraries as in use while protecting the previous deployment", () => {
  state.overview.libsCache = [
    { version: "1.2.0", referenced: true, inUse: true },
    { version: "1.1.7", referenced: true, inUse: false },
  ];

  render(<XXMIDashboard />);

  expect(screen.getByText("1.2.0 · page.setting.xxmi.builtin.inUse")).toBeTruthy();
  expect(screen.getByText("1.1.7")).toBeTruthy();
  expect(screen.queryByText("1.1.7 · page.setting.xxmi.builtin.inUse")).toBeNull();
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.prune" })).toHaveProperty(
    "disabled",
    true,
  );
});

it("shows the external launcher settings instead of the built-in runtime in external mode", () => {
  state.launcherMode = "external";

  render(<XXMIDashboard />);

  expect(screen.getByDisplayValue("C:\XXMI Launcher")).toBeTruthy();
  expect(screen.getByText("page.setting.xxmi.activeImporter")).toBeTruthy();
  expect(screen.queryByText("page.setting.xxmi.builtin.root")).toBeNull();
  cleanup();

  render(<XXMILayout />);

  expect(screen.queryByText("page.setting.xxmi.builtin.importers")).toBeNull();
  expect(
    screen.getByRole("button", { name: "page.setting.xxmi.launcherMode.external" }),
  ).toHaveProperty("disabled", true);
});

it("reveals disabled external importers only when asked", () => {
  state.launcherMode = "external";

  render(<XXMIDashboard />);

  expect(screen.queryByText("SRMI")).toBeNull();

  fireEvent.click(
    screen.getByRole("checkbox", { name: "page.setting.xxmi.showDisabledImporters" }),
  );

  expect(screen.getByText("SRMI").closest("button")?.getAttribute("aria-disabled")).toBe("true");
});

it("keeps the package controls visible when a cache is damaged", () => {
  render(<XXMIDashboard />);

  expect(screen.getByRole("alert").textContent).toContain("missing source.json");
});

it("lists importer status next to the page and disables a running importer", () => {
  render(<XXMILayout />);

  expect(screen.getByText("page.setting.xxmi.builtin.importers")).toBeTruthy();
  cleanup();

  render(<XXMIImporterList />);

  expect(
    screen.queryByRole("button", { name: "page.setting.xxmi.builtin.updateAvailable" }),
  ).toBeNull();
  expect(screen.getByText("page.setting.xxmi.builtin.running")).toBeTruthy();
  expect(screen.getByText("page.setting.xxmi.builtin.customDll")).toBeTruthy();
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.launch" })).toHaveProperty(
    "disabled",
    true,
  );
});

it("offers an importer's installable updates in a dialog before the launch button", () => {
  state.overview.importers[0].running = false;
  state.updates = [
    {
      importer: "GIMI",
      package: "importer:GIMI",
      installed: "1.2.3",
      latestVersion: "1.3.0",
      pinned: false,
      available: true,
    },
    {
      importer: "GIMI",
      package: "xxmi-libs",
      installed: "1.7.5",
      latestVersion: "1.7.6",
      pinned: true,
      available: true,
    },
  ];

  render(<XXMIImporterList />);

  const update = screen.getByRole("button", { name: "page.setting.xxmi.builtin.updateAvailable" });
  const launch = screen.getByRole("button", { name: "page.setting.xxmi.builtin.launch" });
  expect(update.compareDocumentPosition(launch) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.queryByText("page.setting.xxmi.builtin.updateDialogTitle")).toBeNull();

  fireEvent.click(update);

  expect(screen.getByText("page.setting.xxmi.builtin.updateDialogTitle")).toBeTruthy();
  expect(screen.getByText(/importer:GIMI: 1\.2\.3.*1\.3\.0/)).toBeTruthy();
  expect(screen.queryByText(/xxmi-libs/)).toBeNull();
});

it("offers a shared library update on the manage page only", () => {
  state.overview.importers[0].running = false;
  state.updates = [
    {
      importer: "GIMI",
      package: "xxmi-libs",
      installed: "1.7.5",
      latestVersion: "1.7.6",
      pinned: false,
      available: true,
      shared: true,
    },
  ];

  render(<XXMIImporterList />);

  expect(
    screen.queryByRole("button", { name: "page.setting.xxmi.builtin.updateAvailable" }),
  ).toBeNull();
  cleanup();

  render(<XXMIDashboard />);

  expect(screen.getByText(/xxmi-libs: 1\.7\.5.*1\.7\.6/)).toBeTruthy();
});

it("selects the shared XXMI libraries version from the dashboard", async () => {
  xxmi.SetSharedLibsVersion.mockResolvedValue(undefined);
  render(<XXMIDashboard />);

  const shared = screen.getByRole("combobox", {
    name: /page.setting.xxmi.builtin.sharedLibsVersion/,
  });
  expect(shared.textContent).toContain("page.setting.xxmi.builtin.latest");
  fireEvent.click(shared);
  const version = await screen.findByRole("option", { name: "1.7.5" });
  fireEvent.pointerDown(version, { pointerType: "mouse" });
  fireEvent.click(version);

  expect(xxmi.SetSharedLibsVersion).toHaveBeenCalledWith("1.7.5");
});

it("collapses importers that are not installed", () => {
  render(<XXMIImporterList />);

  const trigger = screen.getByRole("button", {
    name: /page\.setting\.xxmi\.builtin\.notInstalled/,
  });
  expect(trigger.getAttribute("aria-expanded")).toBe("false");
  expect(trigger.textContent).toContain("5");
  expect(screen.getByText("GIMI")).toBeTruthy();
  expect(screen.queryByText("WWMI")).toBeNull();

  fireEvent.click(trigger);

  expect(trigger.getAttribute("aria-expanded")).toBe("true");
  expect(screen.getByText("WWMI")).toBeTruthy();
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
