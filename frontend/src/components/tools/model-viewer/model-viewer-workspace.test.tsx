// @vitest-environment jsdom
import { Mod } from "@bindings/mod";
import { Tools } from "@bindings/tools";
import type { ModelViewerTransport } from "@bindings/tools/model_viewer";
import type { ModViewerTransport } from "@shared/mod-viewer/types";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useImperativeHandle, type ComponentProps } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { useModelViewerAnimationClock } from "./model-viewer-animation-clock";
import { ModelViewerDialog } from "./model-viewer-dialog";
import { ModelViewerMenuBar } from "./model-viewer-menu-bar";
import { normalizeModelViewerTransport } from "./model-viewer-transport";
import { ModelViewerWindow } from "./model-viewer-window";
import { ModelViewerWorkspace } from "./model-viewer-workspace";
import { ThreeModelViewer } from "./three-model-viewer";

const handle = vi.hoisted(() => ({
  captureCameraState: vi.fn(() => null),
  captureSquarePngBlob: vi.fn(async () => new Blob()),
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
    GetModelViewerIneffectiveValues: vi.fn(() =>
      Object.assign(Promise.resolve([]), { cancel: vi.fn() }),
    ),
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
    return (
      <>
        <button onClick={() => props.onLoad?.()}>Load viewer</button>
        <button onClick={() => props.onError?.(new Error("load failed"))}>Fail viewer</button>
      </>
    );
  }),
}));

const payload: ModelViewerTransport = {
  memorySessionId: "workspace",
  modPath: "C:/mod",
  iniPath: "C:/mod/mod.ini",
  inis: [{ path: "C:/mod/mod.ini", prefix: "" }],
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

it("shows a loading indicator until the viewer is ready", async () => {
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
    />,
  );

  expect(screen.getByRole("status", { name: "page.tools.model_viewer.loading" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Load viewer" }));
  await waitFor(() =>
    expect(screen.queryByRole("status", { name: "page.tools.model_viewer.loading" })).toBeNull(),
  );
});

it("replaces the loading indicator with an error when the viewer fails", () => {
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
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Fail viewer" }));
  expect(screen.queryByRole("status", { name: "page.tools.model_viewer.loading" })).toBeNull();
  expect(screen.getByRole("alert").textContent).toBe("page.tools.model_viewer.toast.load_error");
});

it("shows the loading indicator again when the source changes", async () => {
  const dialog = (memorySessionId: string) => (
    <ModelViewerDialog
      open
      onOpenChange={vi.fn()}
      source={{
        mode: "payload",
        transport: normalizeModelViewerTransport({ ...payload, memorySessionId }),
        memorySessionId,
        modPath: "C:/mod",
        name: "Model",
      }}
    />
  );
  const view = render(dialog("first"));
  fireEvent.click(screen.getByRole("button", { name: "Load viewer" }));
  await waitFor(() =>
    expect(screen.queryByRole("status", { name: "page.tools.model_viewer.loading" })).toBeNull(),
  );

  view.rerender(dialog("second"));
  expect(screen.getByRole("status", { name: "page.tools.model_viewer.loading" })).toBeTruthy();
});

it("passes payload animation frames to the viewer without recreating them", () => {
  const transport = normalizeModelViewerTransport(payload);
  render(
    <ModelViewerDialog
      open
      onOpenChange={vi.fn()}
      source={{
        mode: "payload",
        transport,
        memorySessionId: "workspace",
        modPath: "C:/mod",
        name: "Model",
      }}
    />,
  );

  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].animationClip?.frames[0]).toBe(
    transport.animations[0].frames[0],
  );
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

it("saves each toggle under its own name to the INI that declares it", async () => {
  const forms = ["Hsin Part 2", "Hsin-ULT Part 2"];
  render(
    <ModelViewerDialog
      open
      onOpenChange={vi.fn()}
      source={{
        mode: "payload",
        transport: normalizeModelViewerTransport({
          ...payload,
          iniPath: `C:/mod/${forms[0]}/SlapMod.ini`,
          defaultState: {
            [`${forms[0]}/SlapMod::swapvar`]: 1,
            [`${forms[1]}/SlapMod::swapvar`]: 2,
          },
          inis: forms.map((form) => ({
            path: `C:/mod/${form}/SlapMod.ini`,
            prefix: `${form}/SlapMod::`,
          })),
        }),
        memorySessionId: "workspace",
        modPath: "C:/mod",
        name: "Model",
      }}
    />,
  );
  await screen.findByRole("button", { name: "Load viewer" });

  await act(async () => {
    vi.mocked(ModelViewerMenuBar).mock.lastCall![0].onSaveTogglesToIni();
  });
  expect(vi.mocked(Tools.PersistModelViewerToggleState).mock.calls).toEqual([
    [`C:/mod/${forms[0]}/SlapMod.ini`, { swapvar: 1 }],
    [`C:/mod/${forms[1]}/SlapMod.ini`, { swapvar: 2 }],
  ]);
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

it("keeps the selected form for the frames the viewer evaluates", async () => {
  render(<ModelViewerDialog open onOpenChange={vi.fn()} source={createFormSource()} />);

  const formSelect = await screen.findByRole("combobox", {
    name: "page.tools.model_viewer.forms.label",
  });
  expect(formSelect.hasAttribute("data-placeholder")).toBe(false);
  fireEvent.click(formSelect);
  const ult = await screen.findByRole("option", { name: "Ult" });
  fireEvent.mouseEnter(ult);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].payloadEval?.meshes).toMatchObject([
    { id: "base", visible: false },
    { id: "ult", visible: true },
  ]);
  expect(within(formSelect).getByText("page.tools.model_viewer.forms.all")).toBeTruthy();
  expect(ult.getAttribute("aria-selected")).toBe("false");
  fireEvent.mouseLeave(ult);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("");
  fireEvent.mouseEnter(ult);
  fireEvent.pointerDown(ult, { pointerType: "mouse" });
  fireEvent.click(ult);
  await waitFor(() => expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult"));
  fireEvent.mouseLeave(ult);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");
  expect(within(formSelect).getByText("Ult")).toBeTruthy();
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].payloadEval?.meshes).toMatchObject([
    { id: "base", visible: false },
    { id: "ult", visible: true },
  ]);
});

it("previews individual and all forms without changing the selection, toggles, or playback", async () => {
  const source = createFormSource();
  const onFormChange = vi.fn();
  render(
    <ModelViewerWorkspace
      open
      source={source}
      form="Base"
      onFormChange={onFormChange}
      doubleSidedEnabled
      onDoubleSidedChange={vi.fn()}
    />,
  );
  const initialViewer = vi.mocked(ThreeModelViewer).mock.lastCall![0];
  const formSelect = screen.getByRole("combobox", {
    name: "page.tools.model_viewer.forms.label",
  });
  fireEvent.click(formSelect);
  const ult = await screen.findByRole("option", { name: "Ult" });
  fireEvent.mouseEnter(ult);

  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");
  expect(within(formSelect).getByText("Base")).toBeTruthy();
  expect(screen.getByRole("option", { name: "Base" }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByText("Base outfit")).toBeTruthy();
  expect(screen.queryByText("Ult outfit")).toBeNull();
  expect(onFormChange).not.toHaveBeenCalled();
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].animationClip).toBe(
    initialViewer.animationClip,
  );
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].orientation).toBe(
    initialViewer.orientation,
  );
  expect(vi.mocked(useModelViewerAnimationClock).mock.lastCall?.[0].playing).toBe(true);

  act(() => vi.mocked(useModelViewerAnimationClock).mock.lastCall![0].onFrame(1));
  expect(handle.setAnimationFrame).toHaveBeenLastCalledWith(1);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");
  fireEvent.mouseLeave(ult);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Base");

  const all = screen.getByRole("option", { name: "page.tools.model_viewer.forms.all" });
  fireEvent.mouseEnter(all);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("");
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].payloadEval?.meshes).toMatchObject([
    { id: "base", visible: true },
    { id: "ult", visible: true },
  ]);
  fireEvent.mouseLeave(all);
  expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Base");
  expect(onFormChange).not.toHaveBeenCalled();
});

it.each(["escape", "outside", "trigger", "selected item"])(
  "clears a form preview when closing with %s",
  async (closeWith) => {
    render(
      <ModelViewerWorkspace
        open
        source={createFormSource()}
        form="Base"
        doubleSidedEnabled
        onDoubleSidedChange={vi.fn()}
      />,
    );
    const formSelect = screen.getByRole("combobox", {
      name: "page.tools.model_viewer.forms.label",
    });
    fireEvent.click(formSelect);
    const ult = await screen.findByRole("option", { name: "Ult" });
    fireEvent.mouseEnter(ult);
    expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");

    if (closeWith === "escape") {
      fireEvent.keyDown(ult, { key: "Escape" });
    } else if (closeWith === "outside") {
      fireEvent.mouseDown(document.body);
      fireEvent.mouseUp(document.body);
      fireEvent.click(document.body);
    } else if (closeWith === "trigger") {
      fireEvent.click(formSelect);
    } else {
      const base = screen.getByRole("option", { name: "Base" });
      fireEvent.pointerDown(base, { pointerType: "mouse" });
      fireEvent.click(base);
    }

    await waitFor(() => expect(formSelect.getAttribute("aria-expanded")).toBe("false"));
    expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Base");
  },
);

it.each(["source", "close", "external form"])(
  "discards a form preview after a %s change",
  async (change) => {
    const source = createFormSource();
    const props: ComponentProps<typeof ModelViewerWorkspace> = {
      open: true,
      source,
      doubleSidedEnabled: true,
      onDoubleSidedChange: vi.fn(),
    };
    const view = render(<ModelViewerWorkspace {...props} />);
    const formSelect = screen.getByRole("combobox", {
      name: "page.tools.model_viewer.forms.label",
    });
    fireEvent.click(formSelect);
    const ult = await screen.findByRole("option", { name: "Ult" });
    fireEvent.mouseEnter(ult);
    expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe("Ult");

    if (change === "source") {
      view.rerender(<ModelViewerWorkspace {...props} source={createFormSource()} />);
    } else if (change === "close") {
      view.rerender(<ModelViewerWorkspace {...props} open={false} />);
      view.rerender(<ModelViewerWorkspace {...props} />);
    } else {
      view.rerender(<ModelViewerWorkspace {...props} form="Base" />);
    }

    expect(vi.mocked(ThreeModelViewer).mock.lastCall?.[0].form).toBe(
      change === "external form" ? "Base" : "",
    );
  },
);

function createFormSource() {
  const mesh = (id: string, form: string): ModViewerTransport["meshes"][number] => ({
    id,
    component: id,
    form,
    geometryUrl: id,
    conditions: [[]],
    texKey: null,
    textureVariants: [],
    normalMapKey: null,
    normalMapVariants: [],
    lightMapKey: null,
    lightMapVariants: [],
    materialMapKey: null,
    cutoutMapKey: null,
    materialMapVariants: [],
    cutoutMapVariants: [],
    shapeTargets: [],
    positionVariants: [],
  });
  const transport: ModViewerTransport = {
    ...normalizeModelViewerTransport(payload),
    meshes: [mesh("base", "Base"), mesh("ult", "Ult")],
    forms: [
      { id: "Base", label: "Base" },
      { id: "Ult", label: "Ult" },
    ],
    variables: ["Base", "Ult"].map((form) => ({
      id: `${form}::outfit`,
      label: `${form} outfit`,
      form,
      order: 0,
      defaultValue: 0,
      values: [
        { value: 0, label: "Default" },
        { value: 1, label: "Alternate" },
      ],
    })),
  };

  return {
    mode: "payload" as const,
    transport,
    memorySessionId: "workspace",
    modPath: "C:/mod",
    name: "Model",
  };
}
