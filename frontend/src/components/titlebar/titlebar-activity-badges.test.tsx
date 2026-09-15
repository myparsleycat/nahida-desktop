// @vitest-environment jsdom

import type { FixInspectionSnapshot } from "@bindings/tools";
import { useModFixInspectionTitlebarActivity } from "@renderer/hooks/use-mod-fix-inspection";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { WrenchIcon } from "lucide-react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { TitlebarActivityBadges } from "./titlebar-activity-badges";

class ResizeObserverMock {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}
vi.stubGlobal("ResizeObserver", ResizeObserverMock);

const mocks = vi.hoisted(() => ({
  dismissed: [] as string[],
  dismissFails: false,
  listeners: new Map<string, (event: { data: unknown }) => void>(),
  loggerError: vi.fn(),
  refresh: vi.fn(),
}));

vi.mock("@bindings/tools", () => ({
  Tools: {
    DismissFixInspection: (modPath: string) => {
      mocks.dismissed.push(modPath);
      if (mocks.dismissFails) return Promise.reject(new Error("dismiss failed"));
      // Mirrors the backend, which emits the updated snapshot before the call resolves.
      mocks.listeners.get("tools:fix-inspections")?.({ data: { revision: 2, inspections: [] } });
      return Promise.resolve();
    },
    RefreshFixInspections: mocks.refresh,
  },
}));

vi.mock("@renderer/hooks/use-settings", () => ({ useSetting: () => ({ data: true }) }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { error: mocks.loggerError } }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, listener: (event: { data: unknown }) => void) => {
      mocks.listeners.set(name, listener);
      return vi.fn();
    },
  },
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) =>
      opts?.name ? `${key}:${String(opts.name)}` : key,
  }),
}));

const pendingSnapshot: FixInspectionSnapshot = {
  revision: 1,
  inspections: [
    {
      modPath: "E:\\ZZZ\\ModA",
      displayName: "ModA",
      result: {
        needsFix: true,
        importer: "ZZMI",
        toolName: "ZZMI Mod Fixer",
        summary: "Found 1 file with an outdated hash",
        details: ["outdated hash"],
        affectedFiles: ["mod.ini"],
        actionTool: "hash",
      },
    },
  ],
};

const badgeText = "titlebar.activity.modFix.label · ModA";

function Harness() {
  useModFixInspectionTitlebarActivity();
  return <TitlebarActivityBadges />;
}

beforeEach(() => {
  for (const id of Object.keys(titlebarActivityStore.getState().activities)) {
    titlebarActivityStore.getState().removeActivity(id);
  }
  mocks.dismissed = [];
  mocks.dismissFails = false;
  mocks.listeners.clear();
  mocks.loggerError.mockClear();
  mocks.refresh.mockReset();
  mocks.refresh.mockResolvedValue(pendingSnapshot);
});

afterEach(cleanup);

it("drops the fix badge after dismissing its popover notification", async () => {
  render(<Harness />);

  await waitFor(() => {
    expect(screen.getByText(badgeText)).toBeDefined();
  });
  expect(screen.getByText("page.mod.fix_needed_toast.title:ModA")).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "titlebar.activity.modFix.dismiss" }));

  await waitFor(() => {
    expect(screen.queryByText(badgeText)).toBeNull();
  });
  expect(mocks.dismissed).toEqual(["E:\\ZZZ\\ModA"]);
});

it("keeps the fix badge and reports the error when dismissal fails", async () => {
  mocks.dismissFails = true;
  render(<Harness />);

  await waitFor(() => {
    expect(screen.getByText(badgeText)).toBeDefined();
  });

  fireEvent.click(screen.getByRole("button", { name: "titlebar.activity.modFix.dismiss" }));

  await waitFor(() => {
    expect(mocks.loggerError).toHaveBeenCalledWith(
      { error: new Error("dismiss failed"), modPath: "E:\\ZZZ\\ModA" },
      "ModFixInspection:dismiss",
    );
  });
  expect(screen.queryByText(badgeText)).not.toBeNull();
});

it("renders no dismiss action for a popover without a dismiss handler", async () => {
  titlebarActivityStore.getState().upsertActivity({
    id: "mod-fix:E:\\ZZZ\\Manual",
    label: "titlebar.activity.modFix.label",
    detail: "Manual",
    status: "warning",
    icon: WrenchIcon,
    order: 5,
    popover: { title: "page.mod.fix_needed_toast.title:Manual", defaultOpen: true },
  });

  render(<TitlebarActivityBadges />);

  await waitFor(() => {
    expect(screen.getByText("page.mod.fix_needed_toast.title:Manual")).toBeDefined();
  });
  expect(screen.queryByRole("button", { name: "titlebar.activity.modFix.dismiss" })).toBeNull();
});
