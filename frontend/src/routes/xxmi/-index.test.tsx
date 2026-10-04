// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
    sharedCustomDll: "",
    customDlls: [] as Array<{ id: string; name: string }>,
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

const xxmi = vi.hoisted(() => ({
  SetSharedLibsVersion: vi.fn(),
  ImportCustomDLL: vi.fn(),
  SetSharedCustomDLL: vi.fn(),
  GetImporterConfig: vi.fn(),
  SaveImporterConfig: vi.fn(),
}));
const dialog = vi.hoisted(() => ({ ShowOpenDialog: vi.fn() }));
const fileDrop = vi.hoisted(() => ({
  drop: (_drop: { paths: string[]; target: { id: string } }) => {},
}));

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("@bindings/platform", () => ({ Dialog: dialog }));
vi.mock("@renderer/wails/file-drop", () => ({
  FileDropTargetID: { xxmiSharedCustomDll: "shared-dll", xxmiImporterCustomDll: "importer-dll" },
  useWindowFileDrop: (listener: typeof fileDrop.drop) => {
    fileDrop.drop = listener;
  },
}));
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
import { toast } from "sonner";

import { XXMIDashboard } from "./index";
import { XXMILayout } from "./route";

afterEach(() => {
  cleanup();
  state.updates = [];
  state.launcherMode = undefined;
  state.overview.importers.splice(1);
  state.overview.importers[0].running = true;
  state.overview.libsCache = [];
  state.overview.sharedCustomDll = "";
  state.overview.customDlls = [];
  Object.values(xxmi).forEach((mock) => mock.mockReset());
  dialog.ShowOpenDialog.mockReset();
  vi.mocked(toast.error).mockReset();
});

function sharedImporterConfig(unsafeMode: boolean) {
  return { mode: "xxmi", xxmiVersion: { follow: "shared" }, migoto: { unsafeMode } };
}

it("selects a shared DLL and enables unsafe mode without overwriting newer importer settings", async () => {
  xxmi.ImportCustomDLL.mockResolvedValue({ id: "abcdef123456", name: "d3d11.dll" });
  xxmi.GetImporterConfig.mockResolvedValue(sharedImporterConfig(false));
  render(<XXMIDashboard />);

  fileDrop.drop({ paths: ["C:\\Builds\\d3d11.dll"], target: { id: "shared-dll" } });

  await waitFor(() => expect(xxmi.SetSharedCustomDLL).toHaveBeenCalledWith("abcdef123456"));
  expect(xxmi.ImportCustomDLL).toHaveBeenCalledWith("C:\\Builds\\d3d11.dll");
  const confirm = await screen.findByRole("button", {
    name: "page.setting.xxmi.builtin.customDllEnableUnsafeConfirm",
  });
  const current = {
    ...sharedImporterConfig(false),
    importerFolder: "D:\\Updated\\GIMI",
    migoto: { unsafeMode: false, logLevel: "Debug" },
  };
  xxmi.GetImporterConfig.mockResolvedValue(current);
  fireEvent.click(confirm);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith("GIMI", {
      ...current,
      migoto: { ...current.migoto, unsafeMode: true },
    }),
  );
  expect(xxmi.GetImporterConfig).toHaveBeenCalledTimes(2);
});

it.each(["GetImporterConfig", "SaveImporterConfig"] as const)(
  "continues after a %s failure and retries only failed importers with fresh settings",
  async (operation) => {
    state.overview.importers.push({ ...state.overview.importers[0], key: "WWMI" });
    xxmi.ImportCustomDLL.mockResolvedValue({ id: "abcdef123456", name: "d3d11.dll" });
    xxmi.GetImporterConfig.mockResolvedValue(sharedImporterConfig(false));
    xxmi.SaveImporterConfig.mockResolvedValue(undefined);
    render(<XXMIDashboard />);
    fileDrop.drop({ paths: ["C:\\Builds\\d3d11.dll"], target: { id: "shared-dll" } });
    const confirm = await screen.findByRole("button", {
      name: "page.setting.xxmi.builtin.customDllEnableUnsafeConfirm",
    });

    xxmi[operation].mockRejectedValueOnce(new Error("settings unavailable"));
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
        "WWMI",
        expect.objectContaining({ migoto: { unsafeMode: true } }),
      ),
    );
    expect(toast.error).toHaveBeenCalledWith("GIMI: settings unavailable");
    await waitFor(() => expect(confirm).toHaveProperty("disabled", false));
    expect(screen.getByRole("alertdialog")).toBeTruthy();

    xxmi.GetImporterConfig.mockClear();
    xxmi.SaveImporterConfig.mockClear();
    const current = { ...sharedImporterConfig(false), importerFolder: "D:\\Retry\\GIMI" };
    xxmi.GetImporterConfig.mockResolvedValue(current);
    fireEvent.click(confirm);

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(xxmi.GetImporterConfig).toHaveBeenCalledExactlyOnceWith("GIMI");
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledExactlyOnceWith("GIMI", {
      ...current,
      migoto: { ...current.migoto, unsafeMode: true },
    });
  },
);

it("selects the shared custom DLL through the file dialog without asking when unsafe mode is on", async () => {
  dialog.ShowOpenDialog.mockResolvedValue({ canceled: false, filePaths: ["D:\\custom.dll"] });
  xxmi.ImportCustomDLL.mockResolvedValue({ id: "abcdef123456", name: "custom.dll" });
  xxmi.GetImporterConfig.mockResolvedValue(sharedImporterConfig(true));
  render(<XXMIDashboard />);

  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.customDllSelect" }),
  );

  await waitFor(() => expect(xxmi.GetImporterConfig).toHaveBeenCalledWith("GIMI"));
  expect(dialog.ShowOpenDialog).toHaveBeenCalledWith(
    expect.objectContaining({ filters: [{ name: "DLL", extensions: ["dll"] }] }),
  );
  expect(xxmi.SetSharedCustomDLL).toHaveBeenCalledWith("abcdef123456");
  expect(
    screen.queryByRole("button", {
      name: "page.setting.xxmi.builtin.customDllEnableUnsafeConfirm",
    }),
  ).toBeNull();
});

it("rejects dropped files that are not a single DLL", () => {
  render(<XXMIDashboard />);

  fileDrop.drop({ paths: ["C:\\Builds\\d3d11.zip"], target: { id: "shared-dll" } });
  fileDrop.drop({ paths: ["C:\\a.dll", "C:\\b.dll"], target: { id: "shared-dll" } });
  fileDrop.drop({ paths: ["C:\\a.dll"], target: { id: "another-target" } });

  expect(toast.error).toHaveBeenCalledTimes(2);
  expect(xxmi.ImportCustomDLL).not.toHaveBeenCalled();
});

it("shows the shared custom DLL and clears it", async () => {
  state.overview.sharedCustomDll = "abcdef123456";
  state.overview.customDlls = [{ id: "abcdef123456", name: "custom.dll" }];
  xxmi.SetSharedCustomDLL.mockResolvedValue(undefined);
  render(<XXMIDashboard />);

  expect(screen.getByText("custom.dll · abcdef123456")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.customDllClear" }));

  await waitFor(() => expect(xxmi.SetSharedCustomDLL).toHaveBeenCalledWith(""));
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
