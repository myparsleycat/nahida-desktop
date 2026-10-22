// @vitest-environment jsdom

import { Setting } from "@bindings/setting";
import { useSettings, useSettingUpdateSync } from "@renderer/hooks/use-settings";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

const listeners = vi.hoisted(() => new Map<string, (event: { data: unknown }) => void>());

vi.mock("@bindings/setting", () => ({ Setting: { GetMany: vi.fn() } }));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, listener: (event: { data: unknown }) => void) => {
      listeners.set(name, listener);
      return () => listeners.delete(name);
    },
  },
}));

const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  client.clear();
  listeners.clear();
  vi.resetAllMocks();
});

it("refreshes cached settings of consumers that were unmounted when the setting changed", async () => {
  let stored = true;
  vi.mocked(Setting.GetMany).mockImplementation(async () => ({
    "mod.gridModelPreview": stored,
    "modelViewer.exposure": 1,
  }));
  const config = { enabled: "mod.gridModelPreview", exposure: "modelViewer.exposure" } as const;
  renderHook(useSettingUpdateSync, { wrapper });
  const grid = renderHook(() => useSettings(config), { wrapper });
  await waitFor(() => expect(grid.result.current.settings.enabled).toBe(true));
  grid.unmount();

  stored = false;
  await act(async () => {
    listeners.get("setting:update")?.({ data: { key: "mod.gridModelPreview", value: false } });
  });
  const reopened = renderHook(() => useSettings(config), { wrapper });
  await waitFor(() => expect(reopened.result.current.settings.enabled).toBe(false));
  expect(client.getQueryData(["settings", "mod.gridModelPreview"])).toBe(false);

  stored = true;
  await act(async () => {
    listeners.get("setting:update")?.({ data: { key: "mod_grid_model_preview", value: "true" } });
  });
  await waitFor(() => expect(reopened.result.current.settings.enabled).toBe(true));
  expect(client.getQueryState(["settings", "mod.gridModelPreview"])?.isInvalidated).toBe(true);
});
