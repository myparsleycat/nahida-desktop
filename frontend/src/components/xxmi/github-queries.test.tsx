// @vitest-environment jsdom

import { XXMI } from "@bindings/xxmi";
import {
  QueryClient,
  QueryClientProvider,
  focusManager,
  onlineManager,
} from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useXXMIUpdates } from "./xxmi-importer-list";

vi.mock("@bindings/xxmi", () => ({ XXMI: { CheckUpdates: vi.fn() } }));
vi.mock("@renderer/hooks/use-launch-guard", () => ({ useLaunchGuard: vi.fn() }));
vi.mock("@renderer/components/game-icon", () => ({ GameIcon: () => null }));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn() } }));

let client: QueryClient;
let now: number;
const hour = 60 * 60 * 1000;
const updatesKey = ["xxmi:updates"];

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  now = new Date("2026-10-04T00:00:00Z").getTime();
  vi.spyOn(Date, "now").mockImplementation(() => now);

  // Enable immediate retries globally so a missing per-query override fails the regression test.
  client = new QueryClient({
    defaultOptions: { queries: { retry: 3, retryDelay: 0, gcTime: Infinity } },
  });
  vi.mocked(XXMI.CheckUpdates).mockResolvedValue([]);
});

afterEach(() => {
  cleanup();
  client.clear();
  focusManager.setFocused(undefined);
  onlineManager.setOnline(true);
  vi.restoreAllMocks();
  vi.resetAllMocks();
});

describe("GitHub update queries", () => {
  it("reuses fresh results on remount, focus, and reconnect for one hour", async () => {
    const first = renderHook(() => useXXMIUpdates(true), { wrapper });
    await waitFor(() => expect(first.result.current).toEqual([]));
    first.unmount();
    now += hour - 1;
    const second = renderHook(() => useXXMIUpdates(true), { wrapper });

    await act(async () => {
      focusManager.setFocused(false);
      focusManager.setFocused(true);
      onlineManager.setOnline(false);
      onlineManager.setOnline(true);
    });

    expect(second.result.current).toEqual([]);
    expect(XXMI.CheckUpdates).toHaveBeenCalledOnce();
    expect(XXMI.CheckUpdates).toHaveBeenCalledWith(false);
  });

  it("fetches again when the one-hour freshness window expires", async () => {
    const first = renderHook(() => useXXMIUpdates(true), { wrapper });
    await waitFor(() => expect(first.result.current).toEqual([]));
    first.unmount();
    now += hour;
    renderHook(() => useXXMIUpdates(true), { wrapper });

    await waitFor(() => expect(XXMI.CheckUpdates).toHaveBeenCalledTimes(2));
  });

  it("still refreshes fresh results after explicit invalidation", async () => {
    const hook = renderHook(() => useXXMIUpdates(true), { wrapper });
    await waitFor(() => expect(hook.result.current).toEqual([]));

    await act(async () => {
      await client.invalidateQueries({ queryKey: updatesKey });
    });

    expect(XXMI.CheckUpdates).toHaveBeenCalledTimes(2);
  });

  it("attempts a failed GitHub request once and allows explicit recovery", async () => {
    vi.mocked(XXMI.CheckUpdates).mockRejectedValue(new Error("GitHub rate limit"));
    renderHook(() => useXXMIUpdates(true), { wrapper });
    await waitFor(() => expect(client.getQueryState(updatesKey)?.status).toBe("error"));
    expect(XXMI.CheckUpdates).toHaveBeenCalledOnce();

    vi.mocked(XXMI.CheckUpdates).mockResolvedValue([]);
    await act(async () => {
      await client.invalidateQueries({ queryKey: updatesKey });
    });

    expect(client.getQueryState(updatesKey)?.status).toBe("success");
    expect(XXMI.CheckUpdates).toHaveBeenCalledTimes(2);
  });

  it("waits until the GitHub query is enabled", async () => {
    const hook = renderHook(({ enabled }) => useXXMIUpdates(enabled), {
      wrapper,
      initialProps: { enabled: false },
    });
    expect(XXMI.CheckUpdates).not.toHaveBeenCalled();
    hook.rerender({ enabled: true });

    await waitFor(() => expect(hook.result.current).toEqual([]));
    expect(XXMI.CheckUpdates).toHaveBeenCalledOnce();
  });
});
