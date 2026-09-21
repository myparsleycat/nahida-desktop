// @vitest-environment jsdom

import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { ELEVATED_HELPER_ACTIVITY_ID } from "@shared/elevated-helper";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useElevatedHelper } from "./use-elevated-helper";

const mocks = vi.hoisted(() => ({
  getStatus: vi.fn(),
  listeners: new Map<string, (event: { data: unknown }) => void>(),
  setSetting: vi.fn(),
  unsubscribers: new Map<string, ReturnType<typeof vi.fn>>(),
}));

vi.mock("@bindings/platform", () => ({
  Input: {
    GetElevatedHelperStatus: mocks.getStatus,
  },
}));

vi.mock("@renderer/lib/logger", () => ({
  Logger: {
    error: vi.fn(),
  },
}));

vi.mock("@renderer/lib/settings", () => ({
  setSetting: mocks.setSetting,
}));

vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: vi.fn((name: string, listener: (event: { data: unknown }) => void) => {
      mocks.listeners.set(name, listener);
      const unsubscribe = vi.fn();
      mocks.unsubscribers.set(name, unsubscribe);
      return unsubscribe;
    }),
  },
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

function Harness() {
  return useElevatedHelper();
}

function emit(name: string, data: unknown) {
  mocks.listeners.get(name)?.({ data });
}

function resetActivities() {
  for (const id of Object.keys(titlebarActivityStore.getState().activities)) {
    titlebarActivityStore.getState().removeActivity(id);
  }
}

describe("useElevatedHelper", () => {
  beforeEach(() => {
    mocks.getStatus.mockReset();
    mocks.getStatus.mockResolvedValue({ enabled: false, running: false });
    mocks.setSetting.mockReset();
    mocks.setSetting.mockResolvedValue(undefined);
    mocks.listeners.clear();
    mocks.unsubscribers.clear();
    resetActivities();
  });

  afterEach(() => {
    cleanup();
    resetActivities();
  });

  it("shows a titlebar warning when the helper is enabled but not running", async () => {
    mocks.getStatus.mockResolvedValue({ enabled: true, running: false });
    render(<Harness />);

    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
      ).toMatchObject({
        id: ELEVATED_HELPER_ACTIVITY_ID,
        status: "warning",
      });
    });
  });

  it("removes the titlebar warning when the helper starts running", async () => {
    mocks.getStatus.mockResolvedValue({ enabled: true, running: false });
    render(<Harness />);
    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
      ).toBeDefined();
    });

    emit("elevated:status", { enabled: true, running: true });

    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
      ).toBeUndefined();
    });
  });

  it("starts the helper from the titlebar action", async () => {
    mocks.getStatus.mockResolvedValue({ enabled: true, running: false });
    render(<Harness />);
    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
      ).toBeDefined();
    });

    titlebarActivityStore
      .getState()
      .activities[ELEVATED_HELPER_ACTIVITY_ID].popover?.action?.onClick();

    await waitFor(() => {
      expect(mocks.setSetting).toHaveBeenCalledWith("general.elevatedHelperEnabled", true);
    });
  });

  it("keeps both start actions disabled until a status event settles the start", async () => {
    mocks.getStatus.mockResolvedValue({ enabled: true, running: false });
    const pending = Promise.withResolvers<void>();
    mocks.setSetting.mockReturnValue(pending.promise);
    render(<Harness />);
    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
      ).toBeDefined();
    });

    const start = () =>
      titlebarActivityStore
        .getState()
        .activities[ELEVATED_HELPER_ACTIVITY_ID].popover?.action?.onClick();
    start();
    start();

    await waitFor(() => {
      expect(mocks.setSetting).toHaveBeenCalledOnce();
    });
    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID]?.popover?.action
          ?.disabled,
      ).toBe(true);
    });

    await act(async () => {
      pending.resolve();
      await pending.promise;
    });

    expect(
      titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID]?.popover?.action
        ?.disabled,
    ).toBe(true);

    emit("elevated:status", { enabled: true, running: false });

    await waitFor(() => {
      expect(
        titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID]?.popover?.action
          ?.disabled,
      ).toBeFalsy();
    });
  });

  it("does not let a stale snapshot overwrite a newer status event", async () => {
    const pending = Promise.withResolvers<{ enabled: boolean; running: boolean }>();
    mocks.getStatus.mockReturnValue(pending.promise);
    render(<Harness />);
    await waitFor(() => {
      expect(mocks.listeners.has("elevated:status")).toBe(true);
    });

    emit("elevated:status", { enabled: true, running: true });
    await act(async () => {
      pending.resolve({ enabled: true, running: false });
      await pending.promise;
    });

    expect(
      titlebarActivityStore.getState().activities[ELEVATED_HELPER_ACTIVITY_ID],
    ).toBeUndefined();
  });

  it("opens a dialog when an agent tool needs the helper", async () => {
    render(<Harness />);
    await waitFor(() => {
      expect(mocks.listeners.has("agent:update")).toBe(true);
    });

    emit("agent:update", {
      type: "tool-end",
      payload: { error: "ELEVATED_HELPER_REQUIRED: elevated helper is not running" },
    });

    expect(await screen.findByRole("alertdialog")).toBeTruthy();
    expect(screen.getByText("titlebar.activity.elevatedHelper.title")).toBeTruthy();
  });
});
