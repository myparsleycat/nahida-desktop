// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { CharacterSidebarItemGrid } from "./character-sidebar-item-grid";
import { CharacterSidebarItemRow } from "./character-sidebar-item-row";

vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./preview", () => ({ Preview: () => null }));

afterEach(cleanup);

const group = { name: "Costume", mods: [] };

it.each([
  ["row", CharacterSidebarItemRow],
  ["grid", CharacterSidebarItemGrid],
])("names the parent of a sub folder listed at the top level (%s)", (_layout, Layout) => {
  render(<Layout group={group} depth={0} parentGroupName="Diluc" />);

  expect(screen.getByText("Costume")).toBeTruthy();
  expect(screen.getByText("Diluc")).toBeTruthy();
});

it.each([
  ["row", CharacterSidebarItemRow],
  ["grid", CharacterSidebarItemGrid],
])("leaves the parent to the indentation for nested folders (%s)", (_layout, Layout) => {
  render(<Layout group={group} depth={1} parentGroupName="Diluc" />);

  expect(screen.getByText("Costume")).toBeTruthy();
  expect(screen.queryByText("Diluc")).toBeNull();
});
