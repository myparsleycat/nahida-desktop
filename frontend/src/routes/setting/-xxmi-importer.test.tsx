// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  SaveImporterConfig: vi.fn(),
  InstallImporterPackage: vi.fn(),
  RestoreOfficialDLL: vi.fn(),
}));
const overview = vi.hoisted(() => ({
  importers: [] as Array<{ key: string; customDll: boolean }>,
}));

const config = {
  enabled: true,
  importerFolder: "C:\\XXMI\\GIMI",
  gameFolder: "",
  mode: "xxmi",
  packageVersion: { follow: "latest" },
  xxmiVersion: { follow: "latest" },
  legacyRuntime: "",
  overwriteINI: true,
  useLaunchOptions: false,
  launchOptions: "",
  processTimeout: 30,
  processStartMethod: "Native",
  processPriority: "Normal",
  xxmiDLLInitDelay: 0,
  windowMode: "Borderless",
  configureGame: false,
  gimi: null,
  srmi: null,
  himi: null,
  wwmi: null,
  migoto: {
    enforceRendering: false,
    enableHunting: false,
    dumpShaders: false,
    muteWarnings: false,
    callsLogging: false,
    debugLogging: false,
    unsafeMode: false,
  },
  runPreLaunch: { enabled: false, command: "", wait: false },
  customLaunch: { enabled: false, command: "", injectMode: "Hook" },
  runPostLoad: { enabled: false, command: "", wait: false },
  extraLibraries: { enabled: false, paths: [] },
  iniOptimizer: { enabled: false, resetCache: false },
  shortcutPath: "",
};

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("@bindings/mod", () => ({ Mod: {} }));
vi.mock("@bindings/platform", () => ({ Dialog: {} }));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("@renderer/hooks/use-launch-guard", () => ({
  useLaunchGuard: () => ({ startImporter: vi.fn(), launchGuardDialog: null }),
}));
vi.mock("@renderer/components/setting/wwmi-graphics-settings", () => ({
  WWMIGraphicsSettings: () => null,
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({
    data:
      queryKey[0] === "xxmi:config"
        ? config
        : queryKey[0] === "xxmi:overview"
          ? overview
          : queryKey[0] === "xxmi:releases"
            ? [
                { version: "1.0.0", signed: false, notes: "Old unsigned release" },
                { version: "2.0.0", signed: true, notes: "Signed release" },
              ]
            : [],
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (options: object) => ({
    options,
    useParams: () => ({ importer: "GIMI" }),
  }),
  lazyRouteComponent: (component: unknown) => component,
  useNavigate: () => vi.fn(),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }));

import { XXMIImporterSettings } from "@renderer/components/setting/xxmi-importer-settings";

afterEach(() => {
  cleanup();
  xxmi.SaveImporterConfig.mockReset();
  xxmi.InstallImporterPackage.mockReset();
  xxmi.RestoreOfficialDLL.mockReset();
  overview.importers = [];
});

it("requires a fresh unsigned confirmation for each selected release", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.InstallImporterPackage.mockResolvedValue(undefined);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  fireEvent.click(screen.getByRole("button", { name: /^1\.0\.0/ }));
  const install = screen.getByRole("button", { name: "page.setting.xxmi.builtin.install" });
  expect(install).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  expect(install).toHaveProperty("disabled", false);

  fireEvent.click(screen.getByRole("button", { name: /^2\.0\.0/ }));
  expect(
    screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }),
  ).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /^1\.0\.0/ }));
  expect(install).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  fireEvent.click(install);

  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "1.0.0",
      allowUnsigned: true,
    }),
  );

  fireEvent.click(screen.getByRole("button", { name: /^2\.0\.0/ }));
  fireEvent.click(install);
  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "2.0.0",
      allowUnsigned: false,
    }),
  );
});

it("offers restoring the official DLL when the importer uses a custom DLL", async () => {
  overview.importers = [{ key: "GIMI", customDll: true }];
  xxmi.RestoreOfficialDLL.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  expect(screen.getByText("page.setting.xxmi.builtin.customDll")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "page.setting.xxmi.builtin.restoreOfficialDll" }),
  );
  await waitFor(() => expect(xxmi.RestoreOfficialDLL).toHaveBeenCalledWith("GIMI"));
});

it("hides the official DLL restore without a custom DLL", () => {
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  expect(
    screen.queryByRole("button", { name: "page.setting.xxmi.builtin.restoreOfficialDll" }),
  ).toBeNull();
});
