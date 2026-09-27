// @vitest-environment jsdom

import type { ModInfo } from "@renderer/types/mod";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ModIniList } from "./mod-ini-list";

vi.mock("@bindings/platform", () => ({ Shell: { OpenPath: vi.fn() } }));
vi.mock("@renderer/components/ui/dialog", () => {
  const Wrapper = ({ children }: PropsWithChildren) => <div>{children}</div>;
  return {
    Dialog: Wrapper,
    DialogContent: Wrapper,
    DialogDescription: Wrapper,
    DialogHeader: Wrapper,
    DialogTitle: Wrapper,
  };
});
vi.mock("@renderer/components/ui/scroll-area", () => ({
  ScrollArea: ({ children }: PropsWithChildren) => <div>{children}</div>,
}));
vi.mock("./key-recorder", () => ({
  KeyRecorder: ({
    otherKeys,
    onSave,
  }: {
    otherKeys: string[];
    onSave: (value: string) => void;
  }) => (
    <>
      <output data-testid="other-keys">{JSON.stringify(otherKeys)}</output>
      <button type="button" onClick={() => onSave("ctrl new")}>
        Save key
      </button>
    </>
  ),
}));

const mod: ModInfo = {
  id: "mod",
  name: "Test mod",
  path: "C:/Mods/Test",
  isEnabled: true,
  mtime: 1,
  size: 1,
  inis: [
    {
      name: "First.ini",
      path: "First.ini",
      toggleKeys: [
        {
          sectionName: "Shared",
          iniFileName: "First.ini",
          back: "alt own",
          variable: "var1",
          values: [],
        },
        {
          sectionName: "Shared",
          iniFileName: "First.ini",
          key: "ctrl duplicate",
          variable: "var2",
          values: [],
        },
        {
          sectionName: "Other",
          iniFileName: "First.ini",
          key: "ctrl other",
          variable: "var3",
          values: [],
        },
      ],
    },
    {
      name: "Second.ini",
      path: "Second.ini",
      toggleKeys: [
        {
          sectionName: "Shared",
          iniFileName: "Second.ini",
          key: "ctrl second",
          back: "alt second",
          variable: "var4",
          values: [],
        },
        {
          sectionName: "Other",
          iniFileName: "Second.ini",
          back: "alt other",
          variable: "var5",
          values: [],
        },
      ],
    },
  ],
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ModIniList", () => {
  it("defers other-key lookup until opening a key and preserves exclusion by INI and section", () => {
    const onToggleKeyUpdate = vi.fn();
    const lookup = vi.spyOn(mod.inis, "flatMap");
    render(<ModIniList mod={mod} expanded onToggleKeyUpdate={onToggleKeyUpdate} />);

    expect(lookup).not.toHaveBeenCalled();
    fireEvent.click(screen.getAllByRole("button", { name: "Add key" })[0]);

    expect(lookup).toHaveBeenCalledOnce();
    expect(screen.getByTestId("other-keys").textContent).toBe(
      JSON.stringify(["ctrl other", "ctrl second", "alt second", "alt other"]),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save key" }));
    expect(onToggleKeyUpdate).toHaveBeenCalledWith(
      mod.path,
      "First.ini",
      "Shared",
      "key",
      "ctrl new",
    );
  });

  it("uses the same lookup when opening a back key in another INI", () => {
    render(<ModIniList mod={mod} expanded onToggleKeyUpdate={vi.fn()} />);

    const secondShared = screen.getAllByText("Shared")[2].parentElement?.parentElement;
    expect(secondShared).toBeTruthy();
    fireEvent.click(within(secondShared!).getByRole("button", { name: /back/i }));

    expect(screen.getByTestId("other-keys").textContent).toBe(
      JSON.stringify(["alt own", "ctrl duplicate", "ctrl other", "alt other"]),
    );
  });
});
