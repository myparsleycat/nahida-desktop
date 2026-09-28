// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
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
    data: queryKey[0] === "xxmi:overview" ? state.overview : [],
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
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { XXMIDashboard } from "./xxmi";

afterEach(cleanup);

it("keeps the package controls visible when a cache is damaged and disables a running importer", () => {
  render(<XXMIDashboard />);

  expect(screen.getByRole("alert").textContent).toContain("missing source.json");
  expect(screen.getByText(/GIMI.*updateAvailable.*running/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "page.setting.xxmi.builtin.launch" })).toHaveProperty(
    "disabled",
    true,
  );
});
