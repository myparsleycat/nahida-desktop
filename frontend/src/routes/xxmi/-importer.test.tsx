// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  SaveImporterConfig: vi.fn(),
  InstallImporterPackage: vi.fn(),
  RestoreOfficialDLL: vi.fn(),
}));
const mod = vi.hoisted(() => ({
  GetGames: vi.fn(),
  UpdateGame: vi.fn(),
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
vi.mock("@bindings/mod", () => ({ Mod: mod }));
vi.mock("@bindings/platform", () => ({ Dialog: {} }));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("@renderer/hooks/use-launch-guard", () => ({
  useLaunchGuard: () => ({ startImporter: vi.fn(), launchGuardDialog: null }),
}));
vi.mock("@renderer/components/xxmi/wwmi-graphics-settings", () => ({
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

import { XXMIImporterSettings } from "@renderer/components/xxmi/xxmi-importer-settings";

afterEach(() => {
  cleanup();
  xxmi.SaveImporterConfig.mockReset();
  xxmi.InstallImporterPackage.mockReset();
  xxmi.RestoreOfficialDLL.mockReset();
  mod.GetGames.mockReset();
  mod.UpdateGame.mockReset();
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
  expect(mod.GetGames).not.toHaveBeenCalled();
});

const linkedGame = {
  game: "Genshin",
  modFolderPath: "C:\\XXMI\\GIMI\\Mods",
  importer: "GIMI",
  linkedModFolderPath: "C:\\XXMI\\GIMI\\Mods",
  gameInstallPath: "D:\\Genshin Impact",
  gameExecutablePath: "D:\\Genshin Impact\\GenshinImpact.exe",
};

async function installAfterFolderMove() {
  fireEvent.change(screen.getByLabelText("page.setting.xxmi.builtin.importerFolder"), {
    target: { value: "D:\\New\\GIMI" },
  });
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  fireEvent.click(screen.getByRole("button", { name: /^1\.0\.0/ }));
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
  await screen.findByRole("button", { name: "page.setting.xxmi.builtin.updateModPaths" });
}

it("confirms a linked mod folder before installing into a new importer folder", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.InstallImporterPackage.mockResolvedValue(undefined);
  mod.UpdateGame.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([
    linkedGame,
    { ...linkedGame, game: "Other", modFolderPath: "C:\\Other\\Mods" },
  ]);
  render(<XXMIImporterSettings importer="GIMI" />);
  await installAfterFolderMove();
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
  expect(xxmi.InstallImporterPackage).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() =>
    expect(screen.queryByText("page.setting.xxmi.builtin.updateModPathsDescription")).toBeNull(),
  );
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
  expect(xxmi.InstallImporterPackage).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
  await screen.findByRole("button", { name: "page.setting.xxmi.builtin.updateModPaths" });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.updateModPaths" }));
  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "1.0.0",
      allowUnsigned: true,
    }),
  );
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "GIMI",
    expect.objectContaining({
      importerFolder: "D:\\New\\GIMI",
      packageVersion: { pinned: "1.0.0" },
    }),
  );
  expect(mod.UpdateGame).toHaveBeenCalledTimes(1);
  expect(mod.UpdateGame).toHaveBeenCalledWith("Genshin", {
    modFolderPath: "D:\\New\\GIMI\\Mods",
    importer: "GIMI",
    linkedModFolderPath: linkedGame.linkedModFolderPath,
    gameInstallPath: linkedGame.gameInstallPath,
    gameExecutablePath: linkedGame.gameExecutablePath,
  });
});

it("installs without moving mod folders when that choice is confirmed", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.InstallImporterPackage.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([linkedGame]);
  render(<XXMIImporterSettings importer="GIMI" />);
  await installAfterFolderMove();

  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.keepModPaths" }));
  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "1.0.0",
      allowUnsigned: true,
    }),
  );
  expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
    "GIMI",
    expect.objectContaining({ importerFolder: "D:\\New\\GIMI" }),
  );
  expect(mod.UpdateGame).not.toHaveBeenCalled();
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
