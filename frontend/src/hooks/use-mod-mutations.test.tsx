// @vitest-environment jsdom

import type { ModInfo } from "@renderer/types/mod";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { useClassificationMutations, useModMutations } from "./use-mod-mutations";

const backend = vi.hoisted(() => ({
  SetCharacterClassification: vi.fn(),
  Toggle: vi.fn(),
  ExclusiveToggle: vi.fn(),
}));

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

it("sends one toggle while the same mod is already toggling", async () => {
  let finishToggle!: () => void;
  backend.Toggle.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finishToggle = resolve;
      }),
  );
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const card = renderHook(() => useModMutations(), { wrapper });
  const shortcut = renderHook(() => useModMutations(), { wrapper });
  const mod = { path: "mods/Diluc/Coat" } as ModInfo;

  let first!: Promise<unknown>;
  let second!: Promise<unknown>;
  act(() => {
    first = card.result.current.toggleModMutation.mutateAsync(mod);
    second = shortcut.result.current.exclusiveToggleModMutation.mutateAsync(mod);
  });
  await act(async () => {
    await vi.waitFor(() => expect(finishToggle).toBeTypeOf("function"));
    finishToggle();
    await Promise.all([first, second]);
  });

  expect(backend.Toggle).toHaveBeenCalledOnce();
  expect(backend.ExclusiveToggle).not.toHaveBeenCalled();
  client.clear();
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
