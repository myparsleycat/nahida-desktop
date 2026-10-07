// @vitest-environment jsdom

import { Setting } from "@bindings/setting";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type ComponentType, Suspense } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Route } from "./gen";

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  changeLanguage: vi.fn(),
  appStatus: { supportsAutostart: false },
}));

vi.mock("@bindings/infra", () => ({ Updater: {} }));
vi.mock("@bindings/setting", () => ({ Setting: { GetImageCacheSize: vi.fn() } }));
// Both routes to the i18n instance share one spy: the hook result and the module singleton.
vi.mock("@renderer/lib/i18n", () => ({
  default: { changeLanguage: mocks.changeLanguage, t: (key: string) => key },
}));
vi.mock("@renderer/components/theme-provider", () => ({
  useTheme: () => ({ theme: "system", setTheme: vi.fn() }),
}));
vi.mock("@renderer/components/ui/select", () => ({
  Select: (props: { name?: string; onValueChange: (value: string | null) => void }) => (
    <button type="button" onClick={() => props.onValueChange("ja")}>
      select:{props.name}
    </button>
  ),
  SelectContent: () => null,
  SelectGroup: () => null,
  SelectItem: () => null,
  SelectTrigger: () => null,
  SelectValue: () => null,
}));
vi.mock("@renderer/hooks/use-auth", () => ({
  useAuth: () => ({ session: null, sessionInitialized: true }),
}));
vi.mock("@renderer/hooks/use-settings", () => ({
  useSettings: () => ({
    settings: {
      runOnStartup: false,
      language: "ko",
      autoUpdateMode: "auto",
      includePrerelease: false,
      runInBackground: true,
      titlebarActivityBadgeClickNavigate: true,
      defaultStartPage: "/mod",
      logLevel: "warn",
      elevatedHelperEnabled: false,
    },
    update: mocks.update,
    isLoading: false,
    setSettings: vi.fn(),
  }),
}));
vi.mock("@renderer/store/global", () => ({
  useGlobalStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      appStatus: mocks.appStatus,
      updateAvailable: false,
      updateDownloaded: false,
      releaseVersion: null,
      shouldPromptForUpdate: false,
      setShouldPromptForUpdate: vi.fn(),
      updaterMode: "auto",
      updaterChecking: false,
      updaterDownloading: false,
      downloadWritten: 0,
      downloadTotal: 0,
      updaterLastError: null,
    }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { changeLanguage: mocks.changeLanguage },
  }),
}));

const RUN_ON_STARTUP = "page.setting.gen.application.runOnStartup";

async function renderGeneralSettings() {
  // The router plugin code-splits route components, so the route exposes a lazy one.
  const Component = Route.options.component as ComponentType & { preload?: () => Promise<void> };
  await Component.preload?.();
  render(
    <Suspense>
      <Component />
    </Suspense>,
  );
  await act(async () => {});
}

beforeEach(() => {
  vi.mocked(Setting.GetImageCacheSize).mockResolvedValue(0);
});

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("general settings language flow", () => {
  it("persists the selection and delegates language application to the global event", async () => {
    await renderGeneralSettings();
    fireEvent.click(screen.getByText("select:language"));

    expect(mocks.update).toHaveBeenCalledOnce();
    expect(mocks.update).toHaveBeenCalledWith("language", "ja");
    expect(mocks.changeLanguage).not.toHaveBeenCalled();
  });
});

describe("general settings run on startup", () => {
  it("exposes the control only for the NSIS install", async () => {
    mocks.appStatus = { supportsAutostart: false };
    await renderGeneralSettings();
    expect(screen.queryByText(RUN_ON_STARTUP)).toBeNull();
    cleanup();

    mocks.appStatus = { supportsAutostart: true };
    await renderGeneralSettings();
    expect(screen.getByText(RUN_ON_STARTUP)).toBeTruthy();
  });
});
