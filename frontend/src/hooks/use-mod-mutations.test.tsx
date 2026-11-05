// @vitest-environment jsdom

import type { ModInfo } from "@renderer/types/mod";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { toast } from "sonner";
import { afterEach, expect, it, vi } from "vitest";

import { useClassificationMutations, useModMutations } from "./use-mod-mutations";

const backend = vi.hoisted(() => ({
  SetCharacterClassification: vi.fn(),
  Toggle: vi.fn(),
  ExclusiveToggle: vi.fn(),
  GetMods: vi.fn(),
}));
const storeState = vi.hoisted(() => ({
  selectedGame: "Game",
  selectedGroup: undefined as { path: string } | undefined,
}));

vi.mock("@bindings/mod", () => ({ Mod: backend }));
vi.mock("@renderer/store/mod", () => ({
  modStore: {},
  useModStore: (select: (state: typeof storeState) => unknown) => select(storeState),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), info: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  storeState.selectedGroup = undefined;
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
    first = card.result.current.toggleModMutation.mutateAsync({ mod, groupPath: undefined });
    second = shortcut.result.current.exclusiveToggleModMutation.mutateAsync({
      mod,
      groupPath: undefined,
    });
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

it("drops an exclusive toggle while another mod in the group is toggling", async () => {
  let finishToggle!: () => void;
  backend.Toggle.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finishToggle = resolve;
      }),
  );
  backend.ExclusiveToggle.mockResolvedValue(undefined);
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useModMutations(), { wrapper });

  let plain!: Promise<unknown>;
  act(() => {
    plain = view.result.current.toggleModMutation.mutateAsync({
      mod: { path: "mods/Diluc/Cape" } as ModInfo,
      groupPath: "mods/Diluc",
    });
  });
  await act(() => vi.waitFor(() => expect(finishToggle).toBeTypeOf("function")));
  let exclusive!: Promise<unknown>;
  act(() => {
    exclusive = view.result.current.exclusiveToggleModMutation.mutateAsync({
      mod: { path: "mods/Diluc/Coat" } as ModInfo,
      groupPath: "mods/Diluc",
    });
  });
  await act(async () => {
    await exclusive;
    finishToggle();
    await plain;
  });

  expect(backend.Toggle).toHaveBeenCalledOnce();
  expect(backend.ExclusiveToggle).not.toHaveBeenCalled();
  expect(toast.info).toHaveBeenCalledOnce();
  client.clear();
});

// An NTE pak wrapper's siblings sit in other wrappers below the displayed group, and a toggle confirmed after
// the user moved to another group still belongs to the group it was requested in.
it("drops a toggle in the requested group while an exclusive toggle renames it", async () => {
  storeState.selectedGroup = { path: "mods/Diluc" };
  backend.Toggle.mockResolvedValue(undefined);
  let finishExclusive!: () => void;
  backend.ExclusiveToggle.mockImplementation(
    () =>
      new Promise<void>((resolve) => {
        finishExclusive = resolve;
      }),
  );
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useModMutations(), { wrapper });

  let exclusive!: Promise<unknown>;
  act(() => {
    exclusive = view.result.current.exclusiveToggleModMutation.mutateAsync({
      mod: { path: "mods/Diluc/WrapperA/Coat" } as ModInfo,
      groupPath: "mods/Diluc",
    });
  });
  await act(() => vi.waitFor(() => expect(finishExclusive).toBeTypeOf("function")));
  storeState.selectedGroup = { path: "mods/Kaeya" };
  view.rerender();
  let sibling!: Promise<unknown>;
  act(() => {
    sibling = view.result.current.toggleModMutation.mutateAsync({
      mod: { path: "mods/Diluc/WrapperB/Cape" } as ModInfo,
      groupPath: "mods/Diluc",
    });
  });
  await act(async () => {
    finishExclusive();
    await Promise.all([exclusive, sibling]);
  });

  expect(backend.ExclusiveToggle).toHaveBeenCalledOnce();
  expect(backend.Toggle).not.toHaveBeenCalled();
  expect(toast.info).toHaveBeenCalledOnce();
  client.clear();
});

it("refreshes the group a toggle was requested in after a selection change", async () => {
  storeState.selectedGroup = { path: "mods/Kaeya" };
  backend.Toggle.mockResolvedValue(undefined);
  backend.GetMods.mockResolvedValue({ path: "mods/Diluc", mods: [] });
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useModMutations(), { wrapper });

  await act(() =>
    view.result.current.toggleModMutation.mutateAsync({
      mod: { path: "mods/Diluc/Coat" } as ModInfo,
      groupPath: "mods/Diluc",
    }),
  );

  expect(backend.GetMods).toHaveBeenCalledExactlyOnceWith("mods/Diluc");
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
