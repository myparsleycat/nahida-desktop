// @vitest-environment jsdom

import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import type { ModInfo } from "@renderer/types/mod";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ModContextMenu } from "./mod-context-menu";

vi.mock("@bindings/mod", () => ({
  Mod: { GetGameBananaModID: vi.fn(() => Promise.resolve(null)) },
}));
vi.mock("@bindings/platform", () => ({
  Shell: { OpenCmd: vi.fn(), OpenExternal: vi.fn(), OpenPath: vi.fn() },
}));
vi.mock("@renderer/lib/agent-navigation", () => ({ openModAgent: vi.fn() }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("@renderer/components/ui/context-menu", () => {
  const Container = ({ children }: PropsWithChildren) => <div>{children}</div>;
  return {
    ContextMenu: Container,
    ContextMenuContent: Container,
    ContextMenuGroup: Container,
    ContextMenuItem: ({ children, onClick }: PropsWithChildren<{ onClick?: () => void }>) => (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
    ContextMenuLabel: Container,
    ContextMenuSeparator: () => null,
    ContextMenuSub: Container,
    ContextMenuSubContent: Container,
    ContextMenuSubTrigger: Container,
    ContextMenuTrigger: Container,
  };
});

const mod: ModInfo = {
  id: "mod",
  name: "Zhuang Fangyi",
  path: "C:/Mods/EFMI/Zhuang Fangyi",
  isEnabled: true,
  mtime: 1,
  size: 1,
  inis: [],
};

function renderMenu(game: { isEfmiGame: boolean; isNteGame: boolean }) {
  const openEfmiOutlineDialog = vi.fn();
  const actions = {
    ...game,
    convertingModelPath: null,
    runner: { presets: [], fixTools: [], modFixer: null, isPreparing: false },
    openEfmiOutlineDialog,
  } as unknown as ModActionApi;
  render(
    <ModContextMenu mod={mod} actions={actions}>
      <span>card</span>
    </ModContextMenu>,
  );
  return openEfmiOutlineDialog;
}

describe("ModContextMenu EFMI outline fix", () => {
  afterEach(cleanup);

  it.each([
    { label: "another importer", isEfmiGame: false, isNteGame: false },
    { label: "NTE", isEfmiGame: false, isNteGame: true },
  ])("is hidden for $label", (game) => {
    renderMenu(game);

    expect(screen.queryByText("page.tools.efmi_outline.title")).toBeNull();
  });

  it("opens the dialog for the right-clicked EFMI mod", () => {
    const openEfmiOutlineDialog = renderMenu({ isEfmiGame: true, isNteGame: false });

    fireEvent.click(screen.getByText("page.tools.efmi_outline.title"));

    expect(openEfmiOutlineDialog).toHaveBeenCalledWith(mod);
  });
});
