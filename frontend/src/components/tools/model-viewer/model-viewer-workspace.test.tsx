// @vitest-environment jsdom
import { Mod } from "@bindings/mod";
import { Tools, type ModelViewerTransport } from "@bindings/tools";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useImperativeHandle, type ComponentProps } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { useModelViewerAnimationClock } from "./model-viewer-animation-clock";
import { ModelViewerDialog } from "./model-viewer-dialog";
import { ModelViewerMenuBar } from "./model-viewer-menu-bar";
import { normalizeModelViewerTransport } from "./model-viewer-transport";
import { ModelViewerWindow } from "./model-viewer-window";
import { ThreeModelViewer } from "./three-model-viewer";

const handle = vi.hoisted(() => ({
  captureCameraState: vi.fn(() => null),
  captureSquarePngDataUrl: vi.fn(async () => "data:image/png;base64,dGVzdA=="),
  restoreCameraState: vi.fn(),
  updateFraming: vi.fn(),
  setDoubleSided: vi.fn(),
  setAnimationFrame: vi.fn(),
}));
vi.mock("@bindings/tools", () => ({
  Tools: {
    LoadModViewer: vi.fn(),
    CleanupModelViewer: vi.fn(),
    PersistModelViewerToggleState: vi.fn(),
  },
}));
vi.mock("@bindings/mod", () => ({ Mod: { PastePreview: vi.fn() } }));
vi.mock("@bindings/platform", () => ({ Shell: { OpenPath: vi.fn() } }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { capture: vi.fn() } }));
vi.mock("@renderer/lib/settings", () => ({
  getSetting: vi.fn(async () => undefined),
  setSetting: vi.fn(async () => undefined),
}));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./model-viewer-animation-clock", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./model-viewer-animation-clock")>()),
  useModelViewerAnimationClock: vi.fn(),
}));
vi.mock("./model-viewer-menu-bar", () => ({
  ModelViewerMenuBar: vi.fn(() => <div data-testid="menus" />),
}));
vi.mock("./three-model-viewer", () => ({
  ThreeModelViewer: vi.fn((props: ComponentProps<typeof ThreeModelViewer>) => {
    useImperativeHandle(props.ref, () => handle, []);
    return <button onClick={() => props.onLoad?.()}>Load viewer</button>;
  }),
}));

const payload: ModelViewerTransport = {
  memorySessionId: "workspace",
  modPath: "C:/mod",
  iniPath: "C:/mod/mod.ini",
  name: "Model",
  previewPath: "C:/mod/preview.jpg",
  meshes: [],
  textures: {},
  variables: [],
  defaultState: { outfit: 1 },
  stateRules: [],
  uiAssets: {},
  computeDeformers: [],
  animations: [
    {
      id: "walk",
      label: "Walk",
      fps: 30,
      frameStart: 0,
      frameEnd: 1,
      loop: true,
      variableIds: [],
      frames: [
        { index: 0, time: 0, values: {} },
        { index: 1, time: 1 / 30, values: {} },
      ],
    },
  ],
};
beforeEach(() => {
  vi.mocked(Tools.LoadModViewer).mockResolvedValue(payload);
  vi.mocked(Tools.CleanupModelViewer).mockResolvedValue(true);
  vi.mocked(Tools.PersistModelViewerToggleState).mockResolvedValue({
    updatedVariables: ["outfit"],
  });
  vi.mocked(Mod.PastePreview).mockResolvedValue("C:/mod/preview.png");
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("preserves double-sided rendering when the dialog body unmounts and reopens", async () => {
  const source = {
    mode: "payload" as const,
    transport: normalizeModelViewerTransport(payload),
    memorySessionId: "workspace",
    modPath: "C:/mod",
    name: "Model",
  };
  const dialog = (open: boolean) => (
    <ModelViewerDialog open={open} onOpenChange={vi.fn()} source={open ? source : null} />
  );
  const view = render(dialog(true));
  await screen.findByRole("button", { name: "Load viewer" });
  act(() => vi.mocked(ModelViewerMenuBar).mock.lastCall![0].onDoubleSidedChange(false));
  view.rerender(dialog(false));
  await waitFor(() => expect(screen.queryByTestId("menus")).toBeNull());
  view.rerender(dialog(true));
  fireEvent.click(await screen.findByRole("button", { name: "Load viewer" }));
  await waitFor(() =>
    expect(vi.mocked(ModelViewerMenuBar).mock.lastCall?.[0].canSaveCapturedPreview).toBe(true),
  );
  expect(vi.mocked(ModelViewerMenuBar).mock.lastCall?.[0].doubleSidedEnabled).toBe(false);
  expect(handle.setDoubleSided).toHaveBeenLastCalledWith(false);
});

it.each(["dialog", "window"])(
  "shares rendering, animation, INI save and overwrite capture in the %s",
  async (host) => {
    const saved = vi.fn();
    if (host === "dialog")
      render(
        <ModelViewerDialog
          open
          onOpenChange={vi.fn()}
          source={{
            mode: "payload",
            transport: normalizeModelViewerTransport(payload),
            memorySessionId: "workspace",
            modPath: "C:/mod",
            name: "Model",
          }}
          existingPreviewPath={payload.previewPath ?? undefined}
          onPreviewSaved={saved}
        />,
      );
    else render(<ModelViewerWindow path="C:/mod" />);
    fireEvent.click(await screen.findByRole("button", { name: "Load viewer" }));
    expect(vi.mocked(useModelViewerAnimationClock).mock.lastCall?.[0].playing).toBe(true);
    await waitFor(() =>
      expect(vi.mocked(ModelViewerMenuBar).mock.lastCall?.[0].canSaveCapturedPreview).toBe(true),
    );
    const menus = () => vi.mocked(ModelViewerMenuBar).mock.lastCall![0];
    await act(async () => {
      menus().onSaveTogglesToIni();
    });
    expect(Tools.PersistModelViewerToggleState).toHaveBeenCalledWith("C:/mod/mod.ini", {
      outfit: 1,
    });
    act(() => menus().onDoubleSidedChange(false));
    expect(handle.setDoubleSided).toHaveBeenLastCalledWith(false);
    act(() => menus().rotateModel([0, 90, 0]));
    expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].orientation).toContain("90");
    fireEvent.change(screen.getByRole("slider"), { target: { value: "1" } });
    expect(vi.mocked(useModelViewerAnimationClock).mock.lastCall?.[0].frameIndex).toBe(1);
    act(() => menus().onCapturePreviewClick());
    expect(Mod.PastePreview).not.toHaveBeenCalled();
    fireEvent.click(
      await screen.findByRole("button", {
        name: "page.tools.model_viewer.dialog.overwrite_preview.confirm",
      }),
    );
    await waitFor(() =>
      expect(Mod.PastePreview).toHaveBeenCalledWith(
        "C:/mod",
        "data:image/png;base64,dGVzdA==",
        "base64",
        "C:/mod/preview.jpg",
      ),
    );
    if (host === "dialog") await waitFor(() => expect(saved).toHaveBeenCalled());
    else {
      await waitFor(() => expect(menus().canSaveCapturedPreview).toBe(true));
      act(() => menus().onCapturePreviewClick());
      fireEvent.click(
        await screen.findByRole("button", {
          name: "page.tools.model_viewer.dialog.overwrite_preview.confirm",
        }),
      );
      await waitFor(() =>
        expect(Mod.PastePreview).toHaveBeenLastCalledWith(
          "C:/mod",
          "data:image/png;base64,dGVzdA==",
          "base64",
          "C:/mod/preview.png",
        ),
      );
    }
  },
);
