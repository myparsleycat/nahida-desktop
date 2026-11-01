// @vitest-environment jsdom

import { Tools } from "@bindings/tools";
import type { ModelViewerTransport } from "@bindings/tools/model_viewer";
import { ModelViewerDialog } from "@renderer/components/tools/model-viewer/model-viewer-dialog";
import type { ModInfo } from "@renderer/types/mod";
import { act, cleanup, renderHook } from "@testing-library/react";
import { Children, isValidElement, StrictMode, type ComponentProps, type ReactNode } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { useModActions, type ModActionApi } from "./use-mod-actions";

vi.mock("@bindings/tools", () => ({
  Tools: { LoadModViewer: vi.fn(), CleanupModelViewer: vi.fn() },
}));
vi.mock("@bindings/mod", () => ({ Mod: {} }));
vi.mock("@renderer/components/tools/model-viewer/model-viewer-dialog", () => ({
  ModelViewerDialog: () => null,
}));
vi.mock("@renderer/hooks/use-confirm-trash", () => ({ useConfirmTrash: () => ({}) }));
vi.mock("@renderer/hooks/use-mod-data", () => ({ useGames: () => ({ data: [] }) }));
vi.mock("@renderer/hooks/use-mod-fix-runner", () => ({ useModFixRunner: () => ({}) }));
vi.mock("@renderer/hooks/use-mod-mutations", () => ({
  useModMutations: () => ({ renameModMutation: { isPending: false } }),
}));
vi.mock("@renderer/store/mod", () => ({ useModStore: () => undefined }));
vi.mock("@renderer/lib/logger", () => ({ Logger: { warn: vi.fn() } }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: [] }) }));
vi.mock("@tanstack/react-router", () => ({ useRouteContext: () => ({ queryClient: {} }) }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("../components/mod/body-shape-dialog", () => ({ BodyShapeDialog: () => null }));
vi.mock("../components/mod/efmi-outline-dialog", () => ({ EfmiOutlineDialog: () => null }));
vi.mock("../components/mod/liquid-emitter-dialog", () => ({ LiquidEmitterDialog: () => null }));
vi.mock("../components/mod/mod-conflict-finder-dialog", () => ({
  ModConflictFinderDialog: () => null,
}));
vi.mock("../components/mod/mod-fix-runner-dialogs", () => ({ ModFixRunnerDialogs: () => null }));
vi.mock("../components/mod/paste-preview", () => ({ pasteModPreview: vi.fn() }));
vi.mock("../components/mod/texture-resize-dialog", () => ({ TextureResizeDialog: () => null }));
vi.mock("../components/mod/touch-profile-dialog", () => ({ TouchProfileDialog: () => null }));

const mod: ModInfo = {
  id: "body",
  name: "Body",
  path: "C:/mods/Body",
  isEnabled: true,
  mtime: 0,
  size: 0,
  inis: [],
};

function payload(memorySessionId: string): ModelViewerTransport {
  return {
    memorySessionId,
    name: mod.name,
    modPath: mod.path,
    iniPath: `${mod.path}/mod.ini`,
    meshes: [],
    textures: {},
    variables: [],
    defaultState: {},
    stateRules: [],
    uiAssets: {},
    animations: [],
    computeDeformers: [],
  };
}

function viewer(actions: ModActionApi) {
  if (!isValidElement<{ children: ReactNode }>(actions.overlays)) {
    throw new Error("Missing overlays");
  }
  const dialog = Children.toArray(actions.overlays.props.children).find(
    (child) => isValidElement(child) && child.type === ModelViewerDialog,
  );
  if (!isValidElement<ComponentProps<typeof ModelViewerDialog>>(dialog)) {
    throw new Error("Missing model viewer dialog");
  }
  return dialog.props;
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.mocked(Tools.CleanupModelViewer).mockResolvedValue(true);
});

afterEach(async () => {
  cleanup();
  await vi.runAllTimersAsync();
  vi.useRealTimers();
  vi.resetAllMocks();
});

it("cleans a closed viewer once even when it immediately unmounts", async () => {
  vi.mocked(Tools.LoadModViewer).mockResolvedValue(payload("one"));
  const view = renderHook(() => useModActions("group"), { wrapper: StrictMode });
  await act(() => view.result.current.openModelViewer(mod));
  expect(viewer(view.result.current).open).toBe(true);
  act(() => viewer(view.result.current).onOpenChange(false));
  view.unmount();
  expect(Tools.CleanupModelViewer).not.toHaveBeenCalled();
  await vi.runAllTimersAsync();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("one");
});

it("cleans a loaded viewer on unmount without a close callback", async () => {
  vi.mocked(Tools.LoadModViewer).mockResolvedValue(payload("loaded"));
  const view = renderHook(() => useModActions("group"));
  await act(() => view.result.current.openModelViewer(mod));
  view.unmount();
  await vi.runAllTimersAsync();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("loaded");
});

it("cleans a load resolving after unmount", async () => {
  const pending = Promise.withResolvers<ModelViewerTransport>();
  vi.mocked(Tools.LoadModViewer).mockImplementationOnce(
    () => pending.promise as ReturnType<typeof Tools.LoadModViewer>,
  );
  const view = renderHook(() => useModActions("group"));
  let load!: Promise<void>;
  act(() => {
    load = view.result.current.openModelViewer(mod);
  });
  view.unmount();
  await act(async () => {
    pending.resolve(payload("late"));
    await load;
  });
  await vi.runAllTimersAsync();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("late");
  expect(toast.error).not.toHaveBeenCalled();
});

it.each(["resolve", "reject"])(
  "ignores an old group load that will %s while a new load is pending",
  async (outcome) => {
    const old = Promise.withResolvers<ModelViewerTransport>();
    const next = Promise.withResolvers<ModelViewerTransport>();
    vi.mocked(Tools.LoadModViewer)
      .mockImplementationOnce(() => old.promise as ReturnType<typeof Tools.LoadModViewer>)
      .mockImplementationOnce(() => next.promise as ReturnType<typeof Tools.LoadModViewer>);
    const view = renderHook(({ group }) => useModActions(group), {
      initialProps: { group: "old" },
    });
    let oldLoad!: Promise<void>;
    act(() => {
      oldLoad = view.result.current.openModelViewer(mod);
    });
    view.rerender({ group: "new" });
    let nextLoad!: Promise<void>;
    act(() => {
      nextLoad = view.result.current.openModelViewer({ ...mod, path: "C:/mods/New" });
    });
    expect(Tools.LoadModViewer).toHaveBeenCalledTimes(2);
    await act(async () => {
      if (outcome === "resolve") old.resolve(payload("old"));
      else old.reject(new Error("old failure"));
      await oldLoad;
    });
    expect(view.result.current.convertingModelPath).toBe("C:/mods/New");
    expect(viewer(view.result.current).open).toBe(false);
    expect(toast.error).not.toHaveBeenCalled();
    await act(async () => {
      next.resolve(payload("new"));
      await nextLoad;
    });
    expect(viewer(view.result.current).source?.memorySessionId).toBe("new");
    await vi.runAllTimersAsync();
    expect(vi.mocked(Tools.CleanupModelViewer).mock.calls).toEqual(
      outcome === "resolve" ? [["old"]] : [],
    );
  },
);

it("clears an already open viewer when the group changes", async () => {
  vi.mocked(Tools.LoadModViewer).mockResolvedValue(payload("old"));
  const view = renderHook(({ group }) => useModActions(group), { initialProps: { group: "old" } });
  await act(() => view.result.current.openModelViewer(mod));
  view.rerender({ group: "new" });
  expect(viewer(view.result.current).open).toBe(false);
  expect(viewer(view.result.current).source).toBeNull();
  await vi.runAllTimersAsync();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("old");
});

it("cleans a response rejected during normalization and allows retry", async () => {
  const invalid = payload("invalid");
  invalid.defaultState = { broken: {} };
  vi.mocked(Tools.LoadModViewer)
    .mockResolvedValueOnce(invalid)
    .mockResolvedValueOnce(payload("retry"));
  const view = renderHook(() => useModActions("group"));
  await act(() => view.result.current.openModelViewer(mod));
  expect(viewer(view.result.current).open).toBe(false);
  expect(toast.error).toHaveBeenCalledTimes(1);
  await vi.runAllTimersAsync();
  expect(Tools.CleanupModelViewer).toHaveBeenCalledExactlyOnceWith("invalid");
  await act(() => view.result.current.openModelViewer(mod));
  expect(viewer(view.result.current).source?.memorySessionId).toBe("retry");
});

it("rejects duplicate calls before React renders and retries a failed load", async () => {
  const pending = Promise.withResolvers<ModelViewerTransport>();
  vi.mocked(Tools.LoadModViewer)
    .mockImplementationOnce(() => pending.promise as ReturnType<typeof Tools.LoadModViewer>)
    .mockResolvedValueOnce(payload("retry"));
  const view = renderHook(() => useModActions("group"));
  let load!: Promise<void>;
  act(() => {
    load = view.result.current.openModelViewer(mod);
    void view.result.current.openModelViewer(mod);
  });
  expect(Tools.LoadModViewer).toHaveBeenCalledTimes(1);
  await act(async () => {
    pending.reject(new Error("load failure"));
    await load;
  });
  expect(view.result.current.convertingModelPath).toBeNull();
  expect(Tools.CleanupModelViewer).not.toHaveBeenCalled();
  await act(() => view.result.current.openModelViewer(mod));
  expect(viewer(view.result.current).source?.memorySessionId).toBe("retry");
});
