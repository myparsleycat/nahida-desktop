import { RootProvider } from "@renderer/components/root-provider";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { useGlobalStore } from "@renderer/store/global";
// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { Route } from "./__root";

const language = vi.hoisted(() => ({ changeLanguage: vi.fn(async () => undefined) }));
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useLocation: () => ({ pathname: "/model-viewer-window" }),
  Outlet: () => <div>Viewer outlet</div>,
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key, i18n: language }),
}));
vi.mock("@renderer/lib/settings", () => ({ getSetting: vi.fn(async () => "ko") }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("@renderer/components/root-provider", () => ({ RootProvider: vi.fn(() => null) }));
vi.mock("@renderer/components/theme-provider", () => ({
  ThemeProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@renderer/components/titlebar/titlebar-window-controls", () => ({
  TitlebarWindowControls: () => <div>Current window controls</div>,
}));
vi.mock("@renderer/components/ui/sonner", () => ({ Toaster: () => <div>Viewer toaster</div> }));
vi.mock("@renderer/store/global", () => ({ useGlobalStore: vi.fn() }));
vi.mock("@renderer/hooks/use-global-events", () => ({ useGlobalEvents: vi.fn() }));
vi.mock("@renderer/hooks/use-mod-events", () => ({
  useDownloadArchiveExtractPromptHandler: vi.fn(),
}));
vi.mock("@renderer/hooks/use-mod-fix-inspection", () => ({
  useModFixInspectionTitlebarActivity: vi.fn(),
}));
vi.mock("@renderer/components/sidebar", () => ({ Sidebar: () => <div>Main sidebar</div> }));
vi.mock("@renderer/components/path-selector-dialog", () => ({ PathSelectorDialog: () => null }));
vi.mock("@renderer/components/update-alert-dialog", () => ({
  UpdateAlertDialog: () => <div>Updater dialog</div>,
}));
vi.mock("@renderer/components/titlebar/titlebar-activity-badges", () => ({
  TitlebarActivityBadges: () => null,
}));
vi.mock("@renderer/components/titlebar/use-4001-fixer-titlebar-activity", () => ({
  use4001FixerTitlebarActivity: vi.fn(),
}));
vi.mock("@renderer/components/titlebar/use-mod-bisect-titlebar-activity", () => ({
  useModBisectTitlebarActivity: vi.fn(),
}));
vi.mock("@renderer/components/titlebar/use-mod-compression-titlebar-activity", () => ({
  useModCompressionTitlebarActivity: vi.fn(),
}));
vi.mock("@renderer/components/titlebar/use-texture-resizer-titlebar-activity", () => ({
  useTextureResizerTitlebarActivity: vi.fn(),
}));
vi.mock("@renderer/components/titlebar/use-transfer-titlebar-activity", () => ({
  useTransferTitlebarActivity: vi.fn(),
}));

afterEach(cleanup);
it("renders only the independent window shell without main providers or global hooks", async () => {
  const Layout = Route.options.component!;
  render(<Layout />);
  expect(screen.getByText("Viewer outlet")).toBeTruthy();
  expect(screen.getByText("Current window controls")).toBeTruthy();
  expect(screen.getByText("Viewer toaster")).toBeTruthy();
  expect(screen.queryByText("Main sidebar")).toBeNull();
  expect(screen.queryByText("Updater dialog")).toBeNull();
  expect(RootProvider).not.toHaveBeenCalled();
  expect(useGlobalEvents).not.toHaveBeenCalled();
  expect(useGlobalStore).not.toHaveBeenCalled();
  await waitFor(() => expect(language.changeLanguage).toHaveBeenCalledWith("ko"));
});
