// @vitest-environment jsdom

import type { FolderGroup } from "@renderer/types/mod";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { CharacterSidebarRow, type CharacterSidebarRowProps } from "./character-sidebar-row";

const store = vi.hoisted(() => ({
  selectedGroup: null as { path: string } | null,
  expandedGroups: new Set<string>(),
  persistentGroups: new Set<string>(),
  setExpandedGroup: vi.fn(),
  toggleCollapsedSection: vi.fn(),
}));
const sectionRows = vi.hoisted(() => ({
  current: [] as {
    kind: "section";
    key: string;
    name: string;
    count: number;
    collapsed: boolean;
  }[],
}));

vi.mock("@renderer/store/mod", () => ({
  useModStore: (select: (state: typeof store) => unknown) => select(store),
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("./use-character-sidebar-visible-rows", () => ({
  useCharacterSidebarVisibleRows: (groups: FolderGroup[]) => [
    ...sectionRows.current,
    ...groups.map((group) => ({ kind: "group", group, depth: 0 })),
  ],
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

class ResizeObserverMock {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}
vi.stubGlobal("ResizeObserver", ResizeObserverMock);

const groups: FolderGroup[] = Array.from({ length: 40 }, (_, index) => ({
  name: `Group ${index}`,
  path: `group-${index}`,
  mods: [],
}));

function createProps(): CharacterSidebarRowProps {
  const viewport = document.createElement("div");
  Object.defineProperties(viewport, {
    clientHeight: { value: 112 },
    offsetHeight: { value: 112 },
    scrollHeight: { value: groups.length * 56 },
  });
  return {
    groups,
    viewport,
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
  sectionRows.current = [];
  vi.stubGlobal("ResizeObserver", ResizeObserverMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("CharacterSidebarRow", () => {
  it("renders only viewport rows with four-row overscan and updates after scrolling", async () => {
    const props = createProps();
    props.onVisibleRowsChange = vi.fn();
    const viewport = props.viewport!;
    document.body.appendChild(viewport);
    const view = render(<CharacterSidebarRow {...props} />, { container: viewport });

    expect(props.onVisibleRowsChange).toHaveBeenCalledWith(
      groups.map((group) => ({ path: group.path, group })),
    );

    expect(screen.getByText("Group 0")).toBeTruthy();
    expect(screen.queryByText("Group 39")).toBeNull();
    expect(view.container.querySelectorAll("[data-index]").length).toBeLessThanOrEqual(6);

    viewport.scrollTop = 35 * 56;
    fireEvent.scroll(viewport);

    await waitFor(() => expect(screen.getByText("Group 35")).toBeTruthy());
    expect(screen.queryByText("Group 0")).toBeNull();
    const mountedIndexes = Array.from(view.container.querySelectorAll("[data-index]"), (element) =>
      Number(element.getAttribute("data-index")),
    );
    expect(mountedIndexes).toEqual(mountedIndexes.toSorted((a, b) => a - b));
    expect(mountedIndexes.length).toBeLessThanOrEqual(10);
    expect(view.container.querySelector("[data-index='35']")?.textContent).toBe("Group 35");
  });

  it("centers a selected row even when it is outside the mounted window", () => {
    const props = createProps();
    const viewport = props.viewport!;
    store.selectedGroup = groups[35];
    viewport.scrollTo = vi.fn();
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
      frames.push(callback);
      return 1;
    });

    document.body.appendChild(viewport);
    render(<CharacterSidebarRow {...props} />, { container: viewport });
    expect(screen.queryByText("Group 35")).toBeNull();
    act(() => frames.forEach((frame) => frame(0)));

    expect(viewport.scrollTo).toHaveBeenCalledWith({ top: 1932, behavior: "auto" });
  });

  it("renders section headers as shorter rows that toggle and stay out of the visible groups", () => {
    const props = createProps();
    props.onVisibleRowsChange = vi.fn();
    const viewport = props.viewport!;
    sectionRows.current = [
      { kind: "section", key: "element:pyro", name: "Pyro", count: 40, collapsed: false },
    ];
    document.body.appendChild(viewport);
    const view = render(<CharacterSidebarRow {...props} />, { container: viewport });

    expect(props.onVisibleRowsChange).toHaveBeenCalledWith(
      groups.map((group) => ({ path: group.path, group })),
    );
    const header = screen.getByRole("button", { name: /Pyro/ });
    expect(header.getAttribute("aria-expanded")).toBe("true");
    expect(header.textContent).toBe("Pyro40");
    expect(view.container.querySelector<HTMLElement>("[data-index='1']")?.style.transform).toBe(
      "translateY(32px)",
    );

    fireEvent.click(header);
    expect(store.toggleCollapsedSection).toHaveBeenCalledWith("element:pyro");
  });
});
