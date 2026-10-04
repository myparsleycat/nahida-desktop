// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { useClassificationMutations } from "./use-mod-mutations";

const backend = vi.hoisted(() => ({ SetCharacterClassification: vi.fn() }));

vi.mock("@bindings/mod", () => ({ Mod: backend }));
vi.mock("@renderer/store/mod", () => ({
  modStore: {},
  useModStore: (select: (state: { selectedGame: string }) => unknown) =>
    select({ selectedGame: "Game" }),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it.each(["pyro", null])("refreshes nested lists after assigning %s", async (groupId) => {
  backend.SetCharacterClassification.mockResolvedValue(undefined);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const keys = [
    ["characters", "Game"],
    ["subGroups", "mods/Collection"],
    ["manualSubGroups", "mods/Collection"],
  ];
  for (const key of keys) {
    client.setQueryData(key, []);
  }
  client.setQueryData(["characters", "Other"], []);
  const { result } = renderHook(() => useClassificationMutations(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  });

  await act(() =>
    result.current.setCharacterClassificationMutation.mutateAsync({
      folderPath: "mods/Collection/Diluc",
      classificationId: "element",
      groupId,
    }),
  );

  expect(backend.SetCharacterClassification).toHaveBeenCalledWith(
    "mods/Collection/Diluc",
    "element",
    groupId,
  );
  for (const key of keys) {
    expect(client.getQueryState(key)?.isInvalidated).toBe(true);
  }
  expect(client.getQueryState(["characters", "Other"])?.isInvalidated).toBe(false);
  client.clear();
});
