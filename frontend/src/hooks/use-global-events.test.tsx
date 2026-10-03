// @vitest-environment jsdom

import { XXMI } from "@bindings/xxmi";
import { useGlobalEvents } from "@renderer/hooks/use-global-events";
import { QueryClient, QueryClientProvider, useQueries, useQuery } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const events = vi.hoisted(() => ({
  listeners: new Map<string, (event: { data: unknown }) => void>(),
  i18n: { t: (key: string) => key, changeLanguage: vi.fn() },
}));

vi.mock("@bindings/auth", () => ({ Auth: {} }));
vi.mock("@bindings/xxmi", () => ({ XXMI: { ListReleases: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ i18n: events.i18n }) }));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (name: string, listener: (event: { data: unknown }) => void) => {
      events.listeners.set(name, listener);
      return () => events.listeners.delete(name);
    },
  },
}));

let client: QueryClient;
let includePrereleases: boolean;
const releaseQueries = [
  { key: ["xxmi:releases", "GIMI"], pkg: "importer:GIMI" },
  { key: ["xxmi:libs-releases"], pkg: "xxmi-libs" },
  { key: ["xxmi:fps-releases"], pkg: "gi-fps-unlocker" },
  { key: ["xxmi:package-releases", "importer:GIMI"], pkg: "importer:GIMI" },
];

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function useReleaseLists() {
  const queries = useQueries({
    queries: releaseQueries.map((query) => ({
      queryKey: query.key,
      queryFn: () => XXMI.ListReleases(query.pkg),
      staleTime: 60 * 60 * 1000,
      retry: false,
    })),
  });
  return queries.map((query) => ({ data: query.data, isSuccess: query.isSuccess }));
}

beforeEach(() => {
  includePrereleases = false;
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  vi.mocked(XXMI.ListReleases).mockImplementation(async () =>
    (includePrereleases ? ["2.0.0-beta", "1.0.0"] : ["1.0.0"]).map((version) => ({
      version,
      tag: `v${version}`,
      publishedAt: "2026-10-04T00:00:00Z",
      prerelease: version.includes("beta"),
      signed: true,
      notes: "",
    })),
  );
});

afterEach(() => {
  cleanup();
  client.clear();
  events.listeners.clear();
  vi.resetAllMocks();
});

it("refilters fresh active and inactive release lists when the persisted prerelease setting changes", async () => {
  const subscription = renderHook(() => useGlobalEvents(), { wrapper });
  const lists = renderHook(useReleaseLists, { wrapper });
  await waitFor(() => expect(lists.result.current.every((query) => query.isSuccess)).toBe(true));
  const dormantKey = ["xxmi:releases", "SRMI"];
  client.setQueryData(dormantKey, [{ version: "1.0.0" }]);
  client.setQueryData(["xxmi:overview"], { configured: true });
  client.setQueryData(["xxmi:updates"], []);
  client.setQueryData(["settings", "general.language"], "en");
  expect(XXMI.ListReleases).toHaveBeenCalledTimes(4);

  includePrereleases = true;
  await act(async () => {
    events.listeners.get("setting:update")?.({
      data: { key: "xxmi.includePrereleases", value: true },
    });
  });
  await waitFor(() =>
    expect(lists.result.current.every((query) => query.data?.[0]?.version === "2.0.0-beta")).toBe(
      true,
    ),
  );
  expect(XXMI.ListReleases).toHaveBeenCalledTimes(8);
  expect(client.getQueryState(dormantKey)?.isInvalidated).toBe(true);
  for (const key of [["xxmi:overview"], ["xxmi:updates"], ["settings", "general.language"]]) {
    expect(client.getQueryState(key)?.isInvalidated).toBe(false);
  }
  const reopened = renderHook(
    () =>
      useQuery({
        queryKey: dormantKey,
        queryFn: () => XXMI.ListReleases("importer:SRMI"),
        staleTime: 60 * 60 * 1000,
      }),
    { wrapper },
  );
  await waitFor(() => expect(reopened.result.current.data?.[0]?.version).toBe("2.0.0-beta"));
  expect(XXMI.ListReleases).toHaveBeenCalledTimes(9);

  includePrereleases = false;
  await act(async () => {
    events.listeners.get("setting:update")?.({
      data: [{ key: "xxmi.includePrereleases", value: false }],
    });
  });
  await waitFor(() =>
    expect(
      [...lists.result.current, reopened.result.current].every(
        (query) => query.data?.length === 1 && query.data[0].version === "1.0.0",
      ),
    ).toBe(true),
  );
  expect(XXMI.ListReleases).toHaveBeenCalledTimes(14);
  subscription.unmount();
  expect(events.listeners.has("setting:update")).toBe(false);
});

it("ignores unrelated or malformed setting events", async () => {
  renderHook(() => useGlobalEvents(), { wrapper });
  const lists = renderHook(useReleaseLists, { wrapper });
  await waitFor(() => expect(lists.result.current.every((query) => query.isSuccess)).toBe(true));
  await act(async () => {
    for (const data of [
      null,
      "xxmi.includePrereleases",
      {},
      [],
      { key: "general.language", value: "ko" },
      { key: "xxmi.autoUpdate", value: "off" },
    ]) {
      events.listeners.get("setting:update")?.({ data });
    }
  });
  expect(XXMI.ListReleases).toHaveBeenCalledTimes(4);
  for (const query of releaseQueries) {
    expect(client.getQueryState(query.key)?.isInvalidated).toBe(false);
  }
});
