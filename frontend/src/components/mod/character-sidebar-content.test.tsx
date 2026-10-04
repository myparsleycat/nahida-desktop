// @vitest-environment jsdom

import type { FolderGroup } from "@renderer/types/mod";
import type { Classification } from "@shared/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { CharacterSidebarGrid } from "./character-sidebar-grid";

const store = vi.hoisted(() => ({
  expandedGroups: new Set<string>(),
  persistentGroups: new Set<string>(),
  collapsedSections: new Set<string>(),
  setExpandedGroup: vi.fn(),
  toggleCollapsedSection: vi.fn(),
}));

vi.mock("@bindings/mod", () => ({ Mod: { GetSubGroups: vi.fn(), GetManualSubGroups: vi.fn() } }));
vi.mock("@renderer/store/mod", () => ({
  useModStore: (select: (state: typeof store) => unknown) => select(store),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./character-sidebar-item", () => ({
  CharacterSidebarItem: ({ group, depth }: { group: FolderGroup; depth: number }) => (
    <div data-depth={depth}>{group.name}</div>
  ),
  CharacterSidebarItemSkeleton: () => null,
}));

afterEach(() => {
  cleanup();
  store.expandedGroups.clear();
  store.collapsedSections.clear();
  vi.clearAllMocks();
});

it.each([false, true])("lists classified grid children in their section (manual: %s)", (manual) => {
  const child: FolderGroup = {
    name: "Diluc",
    path: "mods/Collection/Diluc",
    mods: [],
    classifications: { element: "pyro" },
  };
  const plain: FolderGroup = { name: "Plain", path: "mods/Collection/Plain", mods: [] };
  const parent: FolderGroup = {
    name: "Collection",
    path: "mods/Collection",
    mods: [],
    hasManualSubGroups: manual,
    classifiedSubGroups: [child],
  };
  const grandchild: FolderGroup = { name: "Costume", path: `${child.path}/Costume`, mods: [] };
  const classification: Classification = {
    id: "element",
    name: "Element",
    game: "Game",
    active: true,
    groups: [
      { id: "pyro", name: "Pyro" },
      { id: "hydro", name: "Hydro" },
    ],
  };
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  });
  if (!manual) store.expandedGroups.add(parent.path);
  store.expandedGroups.add(child.path);
  client.setQueryData([manual ? "manualSubGroups" : "subGroups", parent.path], [child, plain]);
  client.setQueryData(["subGroups", child.path], [grandchild]);
  render(
    <QueryClientProvider client={client}>
      <CharacterSidebarGrid
        groups={[parent]}
        classification={classification}
        itemRefs={{ current: new Map() }}
        onItemClick={vi.fn()}
        onItemDrop={vi.fn()}
        searchTerm=""
        sortKey="name"
        sortDirection="ascending"
        hideEmptyGroups={false}
        onCreateFolder={vi.fn()}
        onDeleteFolder={vi.fn()}
        onManualSubGroupChange={vi.fn()}
        showSkeleton={false}
        previewCacheKey={0}
      />
    </QueryClientProvider>,
  );

  expect(screen.getAllByText("Diluc")).toHaveLength(1);
  expect(screen.getByText("Diluc").getAttribute("data-depth")).toBe("0");
  expect(screen.getByText("Costume").getAttribute("data-depth")).toBe("1");
  expect(screen.getByText("Plain").getAttribute("data-depth")).toBe("1");
  expect(screen.getByRole("button", { name: "Hydro0" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Pyro1" }));
  expect(store.toggleCollapsedSection).toHaveBeenCalledWith("element:pyro");
  client.clear();
});
