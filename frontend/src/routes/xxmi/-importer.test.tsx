// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const xxmi = vi.hoisted(() => ({
  SaveImporterConfig: vi.fn(),
  EnsureLibsVersion: vi.fn(),
  EnsureLibsProvider: vi.fn(),
  InstallImporterPackage: vi.fn(),
  RestoreOfficialDLL: vi.fn(),
  ImportCustomDLL: vi.fn(),
}));
const fileDrop = vi.hoisted(() => ({
  drop: (_drop: { paths: string[]; target: { id: string } }) => {},
}));
const mod = vi.hoisted(() => ({
  GetGames: vi.fn(),
  UpdateGame: vi.fn(),
}));
const overview = vi.hoisted(() => ({
  importers: [] as Array<{
    key: string;
    customDll: boolean;
    importerFolder?: string;
    installedVersion?: string;
  }>,
  sharedCustomDll: "",
  sharedLibsVersion: "",
  sharedLibsProvider: "spectrumqt",
  libsProviders: ["spectrumqt", "myparsleycat"],
  customDlls: [] as Array<{ id: string; name: string }>,
}));
const packageVerification = vi.hoisted(() => ({
  data: null as { version: string; method: string } | null,
}));

const config = {
  enabled: true,
  importerFolder: "C:\\XXMI\\GIMI",
  gameFolder: "",
  mode: "xxmi",
  packageVersion: { follow: "latest" },
  xxmiVersion: { follow: "latest" },
  customDll: "",
  legacyRuntime: "",
  overwriteINI: true,
  useLaunchOptions: false,
  launchOptions: "",
  processTimeout: 30,
  gameLaunch: "Direct",
  gameProcessExe: "",
  configurePlatformLaunchOptions: true,
  skipPlatformGameLauncher: true,
  xxmiDLLInjectMode: "Hook",
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
    logLevel: "Disabled",
    clearUnknownSettings: true,
    input: true,
    inputDisableMode: "Mods",
    toggleInput: "ctrl alt shift VK_END",
    unsafeMode: false,
  },
  runPreLaunch: { enabled: false, command: "", wait: false },
  customLaunch: { command: "" },
  runPostLoad: { enabled: false, command: "", wait: false },
  extraLibraries: { enabled: false, paths: [] },
  reshade: { enabled: false },
  iniOptimizer: { enabled: false, resetCache: false },
};

vi.mock("@bindings/xxmi", () => ({ XXMI: xxmi }));
vi.mock("@bindings/mod", () => ({ Mod: mod }));
vi.mock("@bindings/platform", () => ({ Dialog: {} }));
vi.mock("@renderer/wails/file-drop", () => ({
  FileDropTargetID: { xxmiSharedCustomDll: "shared-dll", xxmiImporterCustomDll: "importer-dll" },
  useWindowFileDrop: (listener: typeof fileDrop.drop) => {
    fileDrop.drop = listener;
  },
}));
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
          : queryKey[0] === "xxmi:package-verification"
            ? packageVerification.data
            : queryKey[0] === "xxmi:releases"
              ? [
                  { version: "1.0.0", signed: false, notes: "Old unsigned release" },
                  { version: "2.0.0", signed: true, notes: "Signed release" },
                ]
              : queryKey[0] === "xxmi:libs-releases"
                ? [{ version: "1.7.5" }, { version: "1.7.6" }]
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
  useBlocker: () => ({ status: "idle" }),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }));

import { XXMIImporterSettings } from "@renderer/components/xxmi/xxmi-importer-settings";

function packageRow(version: string) {
  return within(screen.getByRole("button", { name: new RegExp(`^${version}`) }).parentElement!);
}

function openPackageInstall(version: string) {
  fireEvent.click(screen.getByRole("button", { name: new RegExp(`^${version}`) }));
  return within(screen.getByRole("dialog"));
}

it("shows package notes in a dialog without changing the draft", () => {
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  expect(screen.queryByText("Signed release")).toBeNull();
  fireEvent.click(
    packageRow("2.0.0").getByRole("button", {
      name: "page.setting.xxmi.builtin.packageDetails 2.0.0",
    }),
  );
  const dialog = within(screen.getByRole("dialog"));
  expect(dialog.getByText("Signed release")).toBeTruthy();
  expect(xxmi.InstallImporterPackage).not.toHaveBeenCalled();
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
  fireEvent.click(dialog.getByRole("button", { name: "g.cancel" }));
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.getByRole("button", { name: /^2\.0\.0/ }).getAttribute("aria-pressed")).toBe(
    "false",
  );
});

it("asks before installing an uninstalled version and leaves the draft alone on cancel", () => {
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  expect(
    packageRow("2.0.0").queryByRole("button", { name: "page.setting.xxmi.builtin.install" }),
  ).toBeNull();
  const dialog = openPackageInstall("2.0.0");
  expect(dialog.getByText("page.setting.xxmi.builtin.packageNotInstalled")).toBeTruthy();
  expect(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" })).toBeTruthy();
  fireEvent.click(dialog.getByRole("button", { name: "g.cancel" }));
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.getByRole("button", { name: /^2\.0\.0/ }).getAttribute("aria-pressed")).toBe(
    "false",
  );
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeNull();
  expect(xxmi.InstallImporterPackage).not.toHaveBeenCalled();
});

async function failPackageInstall(version: string) {
  xxmi.InstallImporterPackage.mockRejectedValue(new Error("download failed"));
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  const dialog = openPackageInstall(version);
  fireEvent.click(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
  await waitFor(() => expect(xxmi.InstallImporterPackage).toHaveBeenCalled());
  expect(screen.getByRole("dialog")).toBeTruthy();
  fireEvent.click(dialog.getByRole("button", { name: "g.cancel" }));
}

it("keeps the selected package as an unsaved, unsaveable draft when installation fails", async () => {
  await failPackageInstall("2.0.0");
  expect(screen.getByRole("status").textContent).toContain(
    "page.setting.xxmi.builtin.unsavedChanges",
  );
  expect(screen.getAllByRole("button", { name: "g.save" })[0]).toHaveProperty("disabled", true);
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
});

it("returns to follow latest from its row after a failed installation", async () => {
  await failPackageInstall("2.0.0");
  const latest = screen.getByRole("button", { name: "page.setting.xxmi.builtin.latest" });
  expect(within(latest.parentElement!).queryByRole("button", { name: "g.save" })).toBeNull();
  fireEvent.click(latest);
  expect(latest.getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeNull();
});

it("saves a package pin only when that version is already installed in the selected folder", async () => {
  overview.importers = [
    {
      key: "GIMI",
      customDll: false,
      importerFolder: config.importerFolder,
      installedVersion: "2.0.0",
    },
  ];
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  fireEvent.click(screen.getByRole("button", { name: /^2\.0\.0/ }));
  expect(
    packageRow("2.0.0").queryByRole("button", { name: "page.setting.xxmi.builtin.install" }),
  ).toBeNull();
  expect(packageRow("2.0.0").queryByRole("button", { name: "g.save" })).toBeNull();
  expect(screen.getAllByRole("button", { name: "g.save" })[0]).toHaveProperty("disabled", false);
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);
  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({ packageVersion: { pinned: "2.0.0" } }),
    ),
  );
  expect(xxmi.InstallImporterPackage).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).toBeNull();
});

it.each(["GIMI", "SRMI", "WWMI", "ZZMI", "HIMI", "EFMI"])(
  "marks the installed package independently of the selected version for %s",
  (importer) => {
    overview.importers = [
      {
        key: importer,
        customDll: false,
        importerFolder: config.importerFolder,
        installedVersion: "v2.0.0",
      },
    ];
    render(<XXMIImporterSettings importer={importer} />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    const installed = screen.getByRole("button", { name: /^2\.0\.0/ });
    expect(installed.getAttribute("aria-pressed")).toBe("false");
    expect(installed.textContent).toContain("page.setting.xxmi.builtin.packageInstalled");
    expect(screen.getByRole("button", { name: /^1\.0\.0/ }).textContent).not.toContain(
      "page.setting.xxmi.builtin.packageInstalled",
    );
    fireEvent.click(installed);
    expect(installed.getAttribute("aria-pressed")).toBe("true");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeNull();

    const dialog = openPackageInstall("1.0.0");
    expect(installed.textContent).toContain("page.setting.xxmi.builtin.packageInstalled");
    expect(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" })).toBeTruthy();
  },
);

it("recognizes a verified installation even when the importer is disabled in the overview", () => {
  packageVerification.data = { version: "2.0.0", method: "ecdsa" };
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  const installed = screen.getByRole("button", { name: /^2\.0\.0/ });
  expect(installed.textContent).toContain("page.setting.xxmi.builtin.packageInstalled");
  fireEvent.click(installed);
  expect(installed.getAttribute("aria-pressed")).toBe("true");
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("updates the installed marker and install action when installation state refreshes", () => {
  const { rerender } = render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstalled")).toBeNull();
  const dialog = openPackageInstall("2.0.0");
  expect(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" })).toBeTruthy();
  fireEvent.click(dialog.getByRole("button", { name: "g.cancel" }));

  overview.importers = [
    {
      key: "GIMI",
      customDll: false,
      importerFolder: config.importerFolder,
      installedVersion: "2.0.0",
    },
  ];
  rerender(<XXMIImporterSettings importer="GIMI" />);
  expect(screen.getByRole("button", { name: /^2\.0\.0/ }).textContent).toContain(
    "page.setting.xxmi.builtin.packageInstalled",
  );
  fireEvent.click(screen.getByRole("button", { name: /^2\.0\.0/ }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("button", { name: /^2\.0\.0/ }).getAttribute("aria-pressed")).toBe(
    "true",
  );
});

it("requires installation again when the selected importer folder changes", () => {
  overview.importers = [
    {
      key: "GIMI",
      customDll: false,
      importerFolder: config.importerFolder,
      installedVersion: "2.0.0",
    },
  ];
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.change(screen.getByLabelText("page.setting.xxmi.builtin.importerFolder"), {
    target: { value: "D:\\New\\GIMI" },
  });
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstalled")).toBeNull();
  const dialog = openPackageInstall("2.0.0");
  expect(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" })).toBeTruthy();
});

it("discards the pending package selection together with the draft", async () => {
  await failPackageInstall("2.0.0");
  expect(screen.getByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.discard" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.getByRole("button", { name: /^2\.0\.0/ }).getAttribute("aria-pressed")).toBe(
    "false",
  );
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeNull();
});

it("lets an importer whose pinned package is missing be disabled and saved", async () => {
  config.packageVersion = { pinned: "2.0.0" };
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  expect(screen.getByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeTruthy();

  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.enabled" }));
  expect(screen.queryByText("page.setting.xxmi.builtin.packageInstallRequired")).toBeNull();
  expect(screen.getAllByRole("button", { name: "g.save" })[0]).toHaveProperty("disabled", false);
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);
  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({ enabled: false, packageVersion: { pinned: "2.0.0" } }),
    ),
  );
});

afterEach(() => {
  cleanup();
  config.mode = "xxmi";
  config.packageVersion = { follow: "latest" };
  overview.sharedLibsProvider = "spectrumqt";
  overview.sharedLibsVersion = "";
  xxmi.SaveImporterConfig.mockReset();
  xxmi.EnsureLibsVersion.mockReset();
  xxmi.EnsureLibsProvider.mockReset();
  xxmi.InstallImporterPackage.mockReset();
  xxmi.RestoreOfficialDLL.mockReset();
  xxmi.ImportCustomDLL.mockReset();
  mod.GetGames.mockReset();
  mod.UpdateGame.mockReset();
  overview.importers = [];
  overview.sharedCustomDll = "";
  overview.customDlls = [];
  packageVerification.data = null;
});

it("defaults old configs to the existing injector and saves the native selection", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);

  const injector = screen.getByRole("combobox", {
    name: /page.setting.xxmi.builtin.injectionMethod/,
  });
  expect(injector.textContent).toContain("page.setting.xxmi.builtin.injectionDefault");
  fireEvent.click(injector);
  const native = await screen.findByRole("option", {
    name: "page.setting.xxmi.builtin.injectionNative",
  });
  fireEvent.pointerDown(native, { pointerType: "mouse" });
  fireEvent.click(native);
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({ injectionMethod: "Native" }),
    ),
  );
});

async function chooseOption(combobox: RegExp, option: string) {
  fireEvent.click(screen.getByRole("combobox", { name: combobox }));
  const item = await screen.findByRole("option", { name: option });
  fireEvent.pointerDown(item, { pointerType: "mouse" });
  fireEvent.click(item);
}

it("pins the importer's own XXMI version without update notices by default", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.EnsureLibsVersion.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  expect(screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.libsNotify" })).toBeNull();

  await chooseOption(/page.setting.xxmi.builtin.libsVersion/, "1.7.6");
  const notify = screen.getByRole("switch", { name: "page.setting.xxmi.builtin.libsNotify" });
  expect(notify.getAttribute("aria-checked")).toBe("false");
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({
        libsProvider: "spectrumqt",
        xxmiVersion: { pinned: "1.7.6", notify: false },
      }),
    ),
  );
  expect(xxmi.EnsureLibsProvider).toHaveBeenCalledWith("spectrumqt", "1.7.6");
});

it("follows the latest release again when the pinned version's provider changes", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  await chooseOption(/page.setting.xxmi.builtin.libsVersion/, "1.7.6");
  await chooseOption(
    /page.setting.xxmi.builtin.libsProvider/,
    "page.setting.xxmi.builtin.libsProviders.myparsleycat",
  );
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({ libsProvider: "myparsleycat", xxmiVersion: { follow: "latest" } }),
    ),
  );
  expect(xxmi.EnsureLibsVersion).not.toHaveBeenCalled();
});

it("pins a legacy importer's XXMI version to the signed libraries under a shared provider", async () => {
  config.mode = "legacy";
  overview.sharedLibsProvider = "myparsleycat";
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.EnsureLibsVersion.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  await chooseOption(/page.setting.xxmi.builtin.libsVersion/, "1.7.6");
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() => expect(xxmi.SaveImporterConfig).toHaveBeenCalled());
  expect(xxmi.EnsureLibsProvider).toHaveBeenCalledWith("spectrumqt", "1.7.6");
});

it("keeps switched-on update notices when another pinned XXMI version is chosen", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  config.xxmiVersion = { pinned: "1.7.5", notify: true };

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    await chooseOption(/page.setting.xxmi.builtin.libsVersion/, "1.7.6");
    expect(
      screen
        .getByRole("switch", { name: "page.setting.xxmi.builtin.libsNotify" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  } finally {
    config.xxmiVersion = { follow: "latest" };
  }
});

it("follows the shared settings for the provider and its version together", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  config.xxmiVersion = { pinned: "1.7.5", notify: true };

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    // A version of its own under the shared provider is shown as that provider.
    expect(
      screen.getByRole("combobox", { name: /page.setting.xxmi.builtin.libsProvider/ }).textContent,
    ).toContain("page.setting.xxmi.builtin.libsProviders.spectrumqt");

    await chooseOption(
      /page.setting.xxmi.builtin.libsProvider/,
      "page.setting.xxmi.builtin.libsFollowSharedSettings",
    );
    expect(screen.getByText("page.setting.xxmi.builtin.libsSharedSettings")).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.libsNotify" }),
    ).toBeNull();
  } finally {
    config.xxmiVersion = { follow: "latest" };
  }
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({
        libsProvider: "",
        customDll: "",
        xxmiVersion: { follow: "shared" },
      }),
    ),
  );
});

it("keeps the shared version when the importer takes the shared provider as its own", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.EnsureLibsVersion.mockResolvedValue(undefined);
  xxmi.EnsureLibsProvider.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  config.xxmiVersion = { follow: "shared" };
  overview.sharedLibsVersion = "1.7.5";

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    await chooseOption(
      /page.setting.xxmi.builtin.libsProvider/,
      "page.setting.xxmi.builtin.libsProviders.spectrumqt",
    );
    expect(
      screen.getByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }).textContent,
    ).toContain("1.7.5");
    fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

    await waitFor(() =>
      expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
        "GIMI",
        expect.objectContaining({
          libsProvider: "spectrumqt",
          xxmiVersion: { pinned: "1.7.5", notify: false },
        }),
      ),
    );
  } finally {
    config.xxmiVersion = { follow: "latest" };
  }
});

it("keeps a version that alone follows the shared settings until it is changed", async () => {
  Object.assign(config, { libsProvider: "myparsleycat" });
  config.xxmiVersion = { follow: "shared" };

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    const version = screen.getByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ });
    expect(version.textContent).toContain("page.setting.xxmi.builtin.libsFollowShared");
    await chooseOption(/page.setting.xxmi.builtin.libsVersion/, "1.7.6");

    fireEvent.click(version);
    await screen.findByRole("option", { name: "1.7.5" });
    expect(
      screen.queryByRole("option", { name: "page.setting.xxmi.builtin.libsFollowShared" }),
    ).toBeNull();
  } finally {
    Object.assign(config, { libsProvider: "" });
    config.xxmiVersion = { follow: "latest" };
  }
});

it("explains the legacy runtime's XXMI version and hides it for the native injector", () => {
  config.mode = "legacy";

  try {
    const view = render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
    expect(screen.getByText("page.setting.xxmi.builtin.libsVersionLegacy")).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: /page.setting.xxmi.builtin.libsProvider/ }),
    ).toBeNull();
    view.unmount();

    Object.assign(config, { injectionMethod: "Native" });
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
    expect(screen.queryByText("page.setting.xxmi.builtin.libs")).toBeNull();
    expect(
      screen.queryByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }),
    ).toBeNull();
    cleanup();

    config.xxmiVersion = { pinned: "1.7.5", notify: true };
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
    expect(
      screen.getByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }).textContent,
    ).toContain("1.7.5");
  } finally {
    Object.assign(config, { injectionMethod: undefined });
    config.xxmiVersion = { follow: "latest" };
  }
});

it("caches the importer's own libraries provider before saving it", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.EnsureLibsProvider.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  config.xxmiVersion = { follow: "shared" };
  overview.sharedLibsVersion = "1.7.5";

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    const provider = screen.getByRole("combobox", {
      name: /page.setting.xxmi.builtin.libsProvider/,
    });
    expect(provider.textContent).toContain("page.setting.xxmi.builtin.libsFollowSharedSettings");
    await chooseOption(
      /page.setting.xxmi.builtin.libsProvider/,
      "page.setting.xxmi.builtin.libsProviders.myparsleycat",
    );
    fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

    // The shared version names a release of the shared provider, so another provider starts from its latest.
    await waitFor(() =>
      expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
        "GIMI",
        expect.objectContaining({
          libsProvider: "myparsleycat",
          xxmiVersion: { follow: "latest" },
        }),
      ),
    );
    expect(xxmi.EnsureLibsProvider).toHaveBeenCalledWith("myparsleycat", "");
  } finally {
    config.xxmiVersion = { follow: "latest" };
  }
});

it("leaves the draft unsaved when the importer's libraries provider cannot be cached", async () => {
  xxmi.EnsureLibsProvider.mockRejectedValue(new Error("offline"));
  mod.GetGames.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  await chooseOption(
    /page.setting.xxmi.builtin.libsProvider/,
    "page.setting.xxmi.builtin.libsProviders.myparsleycat",
  );
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() => expect(xxmi.EnsureLibsProvider).toHaveBeenCalledWith("myparsleycat", ""));
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
});

it("requires a fresh unsigned confirmation for each selected release", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.InstallImporterPackage.mockResolvedValue(undefined);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

  const dialog = openPackageInstall("1.0.0");
  const install = dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" });
  expect(install).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  expect(install).toHaveProperty("disabled", false);

  fireEvent.click(dialog.getByRole("button", { name: "g.cancel" }));
  const signedDialog = openPackageInstall("2.0.0");
  expect(
    screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }),
  ).toBeNull();
  fireEvent.click(signedDialog.getByRole("button", { name: "g.cancel" }));
  const unsignedDialog = openPackageInstall("1.0.0");
  const unsignedInstall = unsignedDialog.getByRole("button", {
    name: "page.setting.xxmi.builtin.install",
  });
  expect(unsignedInstall).toHaveProperty("disabled", true);
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  fireEvent.click(unsignedInstall);

  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "1.0.0",
      allowUnsigned: true,
      config: expect.objectContaining({ packageVersion: { pinned: "1.0.0" } }),
    }),
  );

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  const nextDialog = openPackageInstall("2.0.0");
  fireEvent.click(nextDialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "2.0.0",
      allowUnsigned: false,
      config: expect.objectContaining({ packageVersion: { pinned: "2.0.0" } }),
    }),
  );
  expect(mod.GetGames).not.toHaveBeenCalled();
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
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
  const dialog = openPackageInstall("1.0.0");
  fireEvent.click(screen.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  fireEvent.click(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
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

  const dialog = openPackageInstall("1.0.0");
  fireEvent.click(dialog.getByRole("switch", { name: "page.setting.xxmi.builtin.allowUnsigned" }));
  fireEvent.click(dialog.getByRole("button", { name: "page.setting.xxmi.builtin.install" }));
  await screen.findByRole("button", { name: "page.setting.xxmi.builtin.updateModPaths" });
  fireEvent.click(screen.getByRole("button", { name: "page.setting.xxmi.builtin.updateModPaths" }));
  await waitFor(() =>
    expect(xxmi.InstallImporterPackage).toHaveBeenCalledWith({
      importer: "GIMI",
      version: "1.0.0",
      allowUnsigned: true,
      config: expect.objectContaining({
        importerFolder: "D:\\New\\GIMI",
        packageVersion: { pinned: "1.0.0" },
      }),
    }),
  );
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
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
      config: expect.objectContaining({ importerFolder: "D:\\New\\GIMI" }),
    }),
  );
  expect(xxmi.SaveImporterConfig).not.toHaveBeenCalled();
  expect(mod.UpdateGame).not.toHaveBeenCalled();
});

it("offers restoring the official DLL when the importer uses a custom DLL", async () => {
  overview.importers = [{ key: "GIMI", customDll: true }];
  xxmi.RestoreOfficialDLL.mockResolvedValue([]);
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

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

it("hides Configure game settings for SRMI, which adjusts no game-side options", () => {
  config.srmi = { unlockFPS: false };

  try {
    render(<XXMIImporterSettings importer="SRMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.gameTweaks" }));

    expect(
      screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.configureGame" }),
    ).toBeNull();
    expect(
      screen.getByRole("switch", { name: "page.setting.xxmi.builtin.unlockFPS" }),
    ).toBeTruthy();
  } finally {
    config.srmi = null;
  }
});

it("hides Configure game settings for GIMI, which always turns off Dynamic Character Resolution", () => {
  config.gimi = { unlockFPS: false, unlockFPSValue: 120, enableHDR: false };

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.gameTweaks" }));

    expect(
      screen.queryByRole("switch", { name: "page.setting.xxmi.builtin.configureGame" }),
    ).toBeNull();
    expect(
      screen.getByRole("switch", { name: "page.setting.xxmi.builtin.enableHDR" }),
    ).toBeTruthy();
  } finally {
    config.gimi = null;
  }
});

it("keeps Configure game settings for WWMI, whose game-side options are optional", () => {
  config.wwmi = {
    unlockFPS: false,
    forceMaxLODBias: false,
    disableWoundedFX: false,
  };

  try {
    render(<XXMIImporterSettings importer="WWMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.gameTweaks" }));

    const toggle = screen.getByRole("switch", { name: "page.setting.xxmi.builtin.configureGame" });
    expect(toggle.getAttribute("aria-checked")).toBe("false");
  } finally {
    config.wwmi = null;
  }
});

async function dropImporterCustomDLL() {
  xxmi.ImportCustomDLL.mockResolvedValue({ id: "abcdef123456", name: "custom.dll" });
  render(<XXMIImporterSettings importer="GIMI" />);
  fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
  await chooseOption(
    /page.setting.xxmi.builtin.libsProvider/,
    "page.setting.xxmi.builtin.customDll",
  );
  await screen.findByRole("button", { name: "page.setting.xxmi.builtin.customDllSelect" });
  fileDrop.drop({ paths: ["C:\\Builds\\custom.dll"], target: { id: "importer-dll" } });
  await waitFor(() => expect(xxmi.ImportCustomDLL).toHaveBeenCalledWith("C:\\Builds\\custom.dll"));
  return screen.findByRole("button", {
    name: "page.setting.xxmi.builtin.customDllEnableUnsafeConfirm",
  });
}

it("saves a dropped custom DLL together with unsafe mode once the prompt is confirmed", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  fireEvent.click(await dropImporterCustomDLL());

  expect(await screen.findByText("custom.dll")).toBeTruthy();
  expect(
    screen.queryByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }),
  ).toBeNull();
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);
  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({
        customDll: "abcdef123456",
        migoto: expect.objectContaining({ unsafeMode: true }),
      }),
    ),
  );
});

it("leaves the draft untouched when the unsafe mode prompt is cancelled", async () => {
  await dropImporterCustomDLL();
  fireEvent.click(screen.getByRole("button", { name: "g.cancel" }));

  await waitFor(() =>
    expect(
      screen.queryByRole("button", {
        name: "page.setting.xxmi.builtin.customDllEnableUnsafeConfirm",
      }),
    ).toBeNull(),
  );
  expect(screen.getByText("page.setting.xxmi.builtin.customDllDropHint")).toBeTruthy();
  expect(screen.queryByText("custom.dll")).toBeNull();
});

it("drops the importer's custom DLL when a provider is chosen again", async () => {
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);
  fireEvent.click(await dropImporterCustomDLL());
  await screen.findByText("custom.dll");

  await chooseOption(
    /page.setting.xxmi.builtin.libsProvider/,
    "page.setting.xxmi.builtin.libsProviders.myparsleycat",
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "page.setting.xxmi.builtin.customDllSelect" }),
    ).toBeNull(),
  );
  fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

  await waitFor(() =>
    expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
      "GIMI",
      expect.objectContaining({ customDll: "", libsProvider: "myparsleycat" }),
    ),
  );
});

it("prepares the provider chosen in place of a saved custom DLL and releases its pin", async () => {
  const migoto = config.migoto;
  config.customDll = "abcdef123456";
  config.migoto = { ...migoto, unsafeMode: true };
  config.xxmiVersion = { pinned: "1.7.6" };
  overview.importers = [{ key: "GIMI", customDll: true }];
  overview.customDlls = [{ id: "abcdef123456", name: "custom.dll" }];
  xxmi.SaveImporterConfig.mockResolvedValue(undefined);
  xxmi.EnsureLibsProvider.mockResolvedValue(undefined);
  mod.GetGames.mockResolvedValue([]);

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));
    await chooseOption(
      /page.setting.xxmi.builtin.libsProvider/,
      "page.setting.xxmi.builtin.libsProviders.myparsleycat",
    );
    fireEvent.click(screen.getAllByRole("button", { name: "g.save" })[0]);

    await waitFor(() =>
      expect(xxmi.SaveImporterConfig).toHaveBeenCalledWith(
        "GIMI",
        expect.objectContaining({
          customDll: "",
          libsProvider: "myparsleycat",
          xxmiVersion: { follow: "latest" },
        }),
      ),
    );
    expect(xxmi.EnsureLibsProvider).toHaveBeenCalledWith("myparsleycat", "");
  } finally {
    config.customDll = "";
    config.migoto = migoto;
    config.xxmiVersion = { follow: "latest" };
  }
});

it("shows the shared custom DLL read-only and warns while unsafe mode is off", () => {
  config.xxmiVersion = { follow: "shared" };
  overview.sharedCustomDll = "abcdef123456";
  overview.customDlls = [{ id: "abcdef123456", name: "shared.dll" }];

  try {
    render(<XXMIImporterSettings importer="GIMI" />);
    fireEvent.click(screen.getByRole("tab", { name: "page.setting.xxmi.builtin.packageTab" }));

    expect(screen.getByText("page.setting.xxmi.builtin.libsSharedSettings")).toBeTruthy();
    expect(screen.getByText("page.setting.xxmi.builtin.customDllInactive")).toBeTruthy();
    expect(
      screen.queryByRole("combobox", { name: /page.setting.xxmi.builtin.libsVersion/ }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "page.setting.xxmi.builtin.customDllSelect" }),
    ).toBeNull();
  } finally {
    config.xxmiVersion = { follow: "latest" };
  }
});
