// @vitest-environment jsdom

import { XXMI } from "@bindings/xxmi";
import { titlebarActivityStore } from "@renderer/store/titlebar-activity";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useXXMITitlebarActivity } from "./use-xxmi-titlebar-activity";

const events = vi.hoisted(() => ({
  listeners: new Map<string, (event: { data: unknown }) => void>(),
}));

vi.mock("@bindings/xxmi", () => ({ XXMI: { CheckUpdates: vi.fn() } }));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, listener: (event: { data: unknown }) => void) => {
      events.listeners.set(name, listener);
      return () => events.listeners.delete(name);
    },
  },
}));

const update = {
  importer: "GIMI",
  package: "importer:GIMI",
  latestVersion: "2.0.0",
  installed: "1.0.0",
  skippedVersion: "",
  pinned: false,
  available: true,
  shared: false,
};
const updatesKey = ["xxmi:updates"];
let client: QueryClient;

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function launchProgress(stage: string, detail?: string, importer = "GIMI") {
  act(() => events.listeners.get("xxmi:launch-progress")?.({ data: { importer, stage, detail } }));
}

function activity(id: string) {
  return titlebarActivityStore.getState().activities[id];
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.mocked(XXMI.CheckUpdates).mockResolvedValue([update]);
});

afterEach(() => {
  cleanup();
  client.clear();
  vi.resetAllMocks();
});

describe("useXXMITitlebarActivity", () => {
  it("shows installable updates until they are gone", async () => {
    renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(activity("xxmi:updates")?.detail).toBe("1"));

    act(() => client.setQueryData(updatesKey, [{ ...update, pinned: true }]));
    await waitFor(() => expect(activity("xxmi:updates")).toBeUndefined());
  });

  it("keeps a launch failure until the importer's next launch starts", async () => {
    renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(activity("xxmi:updates")).toBeDefined());

    launchProgress("auto-update");
    expect(activity("xxmi:update-failure")).toBeUndefined();
    launchProgress("auto-update", "download failed");
    launchProgress("mod-runtimes", "download failed");
    launchProgress("finish");
    expect(activity("xxmi:update-failure")?.href).toBe("/xxmi/GIMI");

    launchProgress("auto-update", undefined, "ZZMI");
    expect(activity("xxmi:update-failure")).toBeDefined();
    launchProgress("auto-update");
    expect(activity("xxmi:update-failure")).toBeUndefined();
  });

  it("drops an update failure once no update is left, but not a mod runtime failure", async () => {
    renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(activity("xxmi:updates")).toBeDefined());

    launchProgress("auto-update", "download failed");
    act(() => client.setQueryData(updatesKey, []));
    await waitFor(() => expect(activity("xxmi:update-failure")).toBeUndefined());

    launchProgress("mod-runtimes", "download failed");
    expect(activity("xxmi:update-failure")?.label).toBe("titlebar.activity.xxmi.modRuntimesFailed");
  });

  it("stops naming a settled update failure beside a mod runtime failure", async () => {
    renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(activity("xxmi:updates")).toBeDefined());

    launchProgress("auto-update", "download failed");
    launchProgress("mod-runtimes", "download failed");
    expect(activity("xxmi:update-failure")?.label).toBe("titlebar.activity.xxmi.updateFailed");

    act(() => client.setQueryData(updatesKey, []));
    await waitFor(() =>
      expect(activity("xxmi:update-failure")?.label).toBe(
        "titlebar.activity.xxmi.modRuntimesFailed",
      ),
    );
  });

  it("keeps an update failure while the update check itself fails", async () => {
    vi.mocked(XXMI.CheckUpdates).mockRejectedValue(new Error("offline"));
    renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(client.getQueryState(updatesKey)?.status).toBe("error"));

    launchProgress("auto-update", "offline");
    expect(activity("xxmi:update-failure")).toBeDefined();
  });

  it("removes its activities when unmounted", async () => {
    const { unmount } = renderHook(() => useXXMITitlebarActivity(), { wrapper });
    await waitFor(() => expect(activity("xxmi:updates")).toBeDefined());
    launchProgress("auto-update", "download failed");

    unmount();

    expect(activity("xxmi:updates")).toBeUndefined();
    expect(activity("xxmi:update-failure")).toBeUndefined();
  });
});
