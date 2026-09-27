// @vitest-environment jsdom

import type { FolderGroup } from "@renderer/types/mod";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CharacterSidebarRow, type CharacterSidebarRowProps } from "./character-sidebar-row";

const store = vi.hoisted(() => ({
  selectedGroup: null as { path: string } | null,
  expandedGroups: new Set<string>(),
  persistentGroups: new Set<string>(),
  setExpandedGroup: vi.fn(),
}));

vi.mock("@renderer/store/mod", () => ({
  useModStore: (select: (state: typeof store) => unknown) => select(store),
}));

vi.mock("./use-character-sidebar-visible-rows", () => ({
  useCharacterSidebarVisibleRows: (groups: FolderGroup[]) =>
    groups.map((group) => ({ group, depth: 0 })),
}));

vi.mock("./character-sidebar-item", () => ({
  CharacterSidebarItem: ({
    group,
    itemRefs,
  }: {
    group: FolderGroup;
    itemRefs: CharacterSidebarRowProps["itemRefs"];
  }) => (
    <button
      type="button"
      data-path={group.path}
      ref={(element) => {
        if (element) {
          itemRefs.current.set(group.path, { element, group });
        } else {
          itemRefs.current.delete(group.path);
        }
      }}
    >
      {group.name}
    </button>
  ),
  CharacterSidebarItemSkeleton: () => <div>Loading</div>,
}));

const groups: FolderGroup[] = Array.from({ length: 40 }, (_, index) => ({
  name: `Group ${index}`,
  path: `group-${index}`,
  mods: [],
}));

function createProps(): CharacterSidebarRowProps {
  return {
    groups,
    viewport: document.createElement("div"),
    itemRefs: { current: new Map() },
    onItemClick: vi.fn(),
    onItemDrop: vi.fn(),
    searchTerm: "",
    sortKey: "name",
    sortDirection: "ascending",
    hideEmptyGroups: false,
    onCreateFolder: vi.fn(),
    onDeleteFolder: vi.fn(),
    onManualSubGroupChange: vi.fn(),
    showSkeleton: false,
    previewCacheKey: 0,
  };
}

beforeEach(() => {
  store.selectedGroup = null;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("CharacterSidebarRow", () => {
  it("renders every visible row without a virtualized window", () => {
    const props = createProps();
    render(<CharacterSidebarRow {...props} />);

    expect(screen.getAllByRole("button")).toHaveLength(groups.length);
    expect(screen.getByText("Group 39")).toBeTruthy();
  });

  it("centers the selected row inside the sidebar viewport", () => {
    const props = createProps();
    const viewport = props.viewport!;
    store.selectedGroup = groups[35];
    viewport.scrollTop = 24;
    Object.defineProperty(viewport, "clientHeight", { value: 112 });
    viewport.scrollTo = vi.fn();
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function () {
      return {
        top: this === viewport ? 20 : this.dataset.path === "group-35" ? 244 : 0,
        height: 56,
      } as DOMRect;
    });
    let frame: FrameRequestCallback | undefined;
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frame = callback;
      return 1;
    });

    render(<CharacterSidebarRow {...props} />);
    act(() => frame?.(0));

    expect(viewport.scrollTo).toHaveBeenCalledWith({ top: 220 });
  });
});
