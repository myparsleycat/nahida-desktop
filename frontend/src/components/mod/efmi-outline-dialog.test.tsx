// @vitest-environment jsdom

import { Tools } from "@bindings/tools";
import { OutlineFileState, type OutlineInspection } from "@bindings/tools/efmi_outline";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EfmiOutlineDialog } from "./efmi-outline-dialog";

vi.mock("@bindings/tools", () => ({
  Tools: { EFMIOutlineInspect: vi.fn(), EFMIOutlineApply: vi.fn() },
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@renderer/components/ui/dialog", () => {
  const Wrapper = ({ children }: PropsWithChildren) => <div>{children}</div>;
  return {
    Dialog: Wrapper,
    DialogContent: Wrapper,
    DialogDescription: Wrapper,
    DialogFooter: Wrapper,
    DialogHeader: Wrapper,
    DialogTitle: Wrapper,
  };
});

function inspectionFor(modPath: string, applied = false): OutlineInspection {
  return {
    modPath,
    files: [
      {
        path: "mod.ini",
        state: applied ? OutlineFileState.StateApplied : OutlineFileState.StateOriginal,
        hasBackup: applied,
        parts: [
          {
            id: `${modPath}|part`,
            section: "CommandList_Draw_Component0",
            label: `$part_of_${modPath}`,
            sections: ["CommandList_Draw_Component0"],
            lines: [3],
            applied,
            locked: false,
          },
        ],
        manual: [],
      },
    ],
  };
}

describe("EfmiOutlineDialog", () => {
  afterEach(cleanup);

  // The dialog stays mounted when another mod is opened, so a request that
  // finishes late must not land in the newly opened mod's cache.
  it("stores a late apply result under the mod it was made for", async () => {
    vi.mocked(Tools.EFMIOutlineInspect).mockImplementation(
      (modPath) =>
        Promise.resolve(inspectionFor(modPath)) as ReturnType<typeof Tools.EFMIOutlineInspect>,
    );
    let finishApply: (result: OutlineInspection) => void = () => {};
    vi.mocked(Tools.EFMIOutlineApply).mockImplementation(
      () =>
        new Promise<OutlineInspection>((resolve) => {
          finishApply = resolve;
        }) as ReturnType<typeof Tools.EFMIOutlineApply>,
    );
    const queryClient = new QueryClient();
    const dialog = (modPath: string) => (
      <QueryClientProvider client={queryClient}>
        <EfmiOutlineDialog open onOpenChange={() => {}} modPath={modPath} modName={modPath} />
      </QueryClientProvider>
    );

    const view = render(dialog("A"));
    await screen.findByText("$part_of_A");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByText("g.apply"));
    await waitFor(() => {
      expect(Tools.EFMIOutlineApply).toHaveBeenCalledWith({
        modPath: "A",
        files: [{ path: "mod.ini", partIds: ["A|part"] }],
      });
    });

    view.rerender(dialog("B"));
    await screen.findByText("$part_of_B");
    finishApply(inspectionFor("A", true));

    await waitFor(() => {
      expect(queryClient.getQueryData(["tools:efmiOutline", "A"])).toEqual(
        inspectionFor("A", true),
      );
    });
    expect(queryClient.getQueryData(["tools:efmiOutline", "B"])).toEqual(inspectionFor("B"));
    expect(screen.getByText("$part_of_B")).toBeTruthy();
  });
});
