// @vitest-environment jsdom

import type { FolderGroup } from "@renderer/types/mod";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { CharacterSidebarItem } from "./character-sidebar-item";

vi.mock("@bindings/platform", () => ({ Shell: { OpenPath: vi.fn() } }));
vi.mock("@renderer/hooks/use-bulk-mod-toggle", () => ({
  useBulkModToggle: () => ({ isPending: false }),
}));
vi.mock("@renderer/wails/file-drop", () => ({
  FILE_DROP_GROUP_PATH_ATTRIBUTE: "data-file-drop-group-path",
  useWindowFileDrop: vi.fn(),
}));
vi.mock("@renderer/store/mod", () => ({
  useModStore: (select: (state: object) => unknown) =>
    select({ selectedGroup: null, expandedGroups: new Set(), persistentGroups: new Set() }),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./character-sidebar-item-row", () => ({ CharacterSidebarItemRow: () => null }));
vi.mock("./character-sidebar-item-grid", () => ({ CharacterSidebarItemGrid: () => null }));
vi.mock("./character-sidebar-classification-menu", () => ({
  CharacterSidebarClassificationMenu: ({ group }: { group: FolderGroup }) => (
    <div>Classify {group.path}</div>
  ),
}));
vi.mock("@renderer/components/ui/context-menu", () => {
  const Container = ({ children }: { children?: ReactNode }) => <div>{children}</div>;
  return {
    ContextMenu: Container,
    ContextMenuContent: Container,
    ContextMenuItem: Container,
    ContextMenuSeparator: () => null,
    ContextMenuSub: Container,
    ContextMenuSubContent: Container,
    ContextMenuSubTrigger: Container,
    ContextMenuTrigger: Container,
  };
});

afterEach(cleanup);

it.each([0, 1, 2])("offers classification at sidebar depth %s", (depth) => {
  const group: FolderGroup = { name: "Diluc", path: "mods/Collection/Diluc", mods: [] };
  render(
    <CharacterSidebarItem
      group={group}
      depth={depth}
      onClick={vi.fn()}
      itemRefs={{ current: new Map() }}
    />,
  );

  expect(screen.getByText(`Classify ${group.path}`)).toBeTruthy();
});
