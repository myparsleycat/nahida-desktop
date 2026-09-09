import { Shell } from "@bindings/platform";
// @vitest-environment jsdom
import { Tools, type ModelViewerTransport } from "@bindings/tools";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { ModelViewerWindow } from "./model-viewer-window";
import { ModelViewerWorkspace } from "./model-viewer-workspace";

vi.mock("@bindings/tools", () => ({
  Tools: { LoadModViewer: vi.fn(), CleanupModelViewer: vi.fn() },
}));
vi.mock("@bindings/platform", () => ({ Shell: { OpenPath: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./model-viewer-workspace", () => ({
  ModelViewerWorkspace: vi.fn(() => <div data-testid="workspace" />),
}));

const payload = (memorySessionId: string): ModelViewerTransport => ({
  memorySessionId,
  modPath: "C:/모드 (1)",
  iniPath: "C:/모드 (1)/mod.ini",
  name: "모드 (1)",
  previewPath: "C:/모드 (1)/preview.jpg",
  meshes: [],
  textures: {},
  variables: [],
  defaultState: {},
  stateRules: [],
  uiAssets: {},
  animations: [],
  computeDeformers: [],
});
beforeEach(() => {
  vi.mocked(Tools.CleanupModelViewer).mockResolvedValue(true);
  vi.mocked(Shell.OpenPath).mockResolvedValue(undefined);
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it("loads into the shared workspace with the existing preview and cleans once on pagehide/unmount", async () => {
  vi.mocked(Tools.LoadModViewer).mockResolvedValue(payload("one"));
  const view = render(<ModelViewerWindow path="C:/모드 (1)" />);
  expect(screen.getByRole("status")).toBeTruthy();
  await screen.findByTestId("workspace");
  expect(Tools.LoadModViewer).toHaveBeenCalledWith("C:/모드 (1)");
  expect(vi.mocked(ModelViewerWorkspace).mock.lastCall?.[0]).toMatchObject({
    open: true,
    existingPreviewPath: "C:/모드 (1)/preview.jpg",
    source: { memorySessionId: "one", modPath: "C:/모드 (1)" },
  });
  fireEvent.click(screen.getByRole("button", { name: "page.tools.model_viewer.open_folder" }));
  expect(Shell.OpenPath).toHaveBeenCalledWith("C:/모드 (1)");
  fireEvent(window, new Event("pagehide"));
  view.unmount();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("one");
});

it("keeps backend errors visible and retries in place", async () => {
  vi.mocked(Tools.LoadModViewer)
    .mockRejectedValueOnce(new Error("No active .ini files found in this folder."))
    .mockResolvedValueOnce(payload("retry"));
  render(<ModelViewerWindow path="C:/invalid" />);
  expect((await screen.findByRole("alert")).textContent).toContain("No active .ini files");
  fireEvent.click(screen.getByRole("button", { name: "page.tools.model_viewer.retry" }));
  await screen.findByTestId("workspace");
  expect(Tools.LoadModViewer).toHaveBeenCalledTimes(2);
});

it("cleans a late result after source changes without replacing the new workspace", async () => {
  const old = Promise.withResolvers<ModelViewerTransport>();
  vi.mocked(Tools.LoadModViewer)
    .mockImplementationOnce(() => old.promise as ReturnType<typeof Tools.LoadModViewer>)
    .mockResolvedValueOnce(payload("new"));
  const view = render(<ModelViewerWindow path="C:/old" />);
  view.rerender(<ModelViewerWindow path="C:/new" />);
  await screen.findByTestId("workspace");
  await act(async () => {
    old.resolve(payload("old"));
    await old.promise;
  });
  await waitFor(() => expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("old"));
  expect(vi.mocked(ModelViewerWorkspace).mock.lastCall?.[0].source?.memorySessionId).toBe("new");
  view.unmount();
  expect(vi.mocked(Tools.CleanupModelViewer).mock.calls).toEqual([["old"], ["new"]]);
});

it("cleans a pending load that resolves after unmount", async () => {
  const pending = Promise.withResolvers<ModelViewerTransport>();
  vi.mocked(Tools.LoadModViewer).mockImplementationOnce(
    () => pending.promise as ReturnType<typeof Tools.LoadModViewer>,
  );
  const view = render(<ModelViewerWindow path="C:/mod" />);
  view.unmount();
  await act(async () => {
    pending.resolve(payload("late"));
    await pending.promise;
  });
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("late");
});

it("rejects an empty folder without starting a backend load", () => {
  render(<ModelViewerWindow path=" " />);
  expect(screen.getByRole("alert")).toBeTruthy();
  expect(Tools.LoadModViewer).not.toHaveBeenCalled();
});
