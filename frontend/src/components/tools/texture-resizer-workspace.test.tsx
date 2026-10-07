// @vitest-environment jsdom

import { Tools } from "@bindings/tools";
import type { TextureResizeListItem, TextureResizeSettings } from "@shared/types";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TextureResizerWorkspace } from "./texture-resizer-workspace";

vi.mock("@bindings/tools", () => ({
  Tools: { GetTextureResizeSettings: vi.fn(), ListTextureMod: vi.fn() },
}));
vi.mock("@bindings/platform", () => ({ Dialog: {}, Shell: {} }));
vi.mock("@renderer/components/tools/texture-resizer-form", () => ({
  TextureResizerForm: () => null,
  formatTextureFormatLabel: (format: string) => format,
}));
vi.mock("@wailsio/runtime", () => ({ Events: { On: () => vi.fn() } }));
vi.mock("react-i18next", () => {
  // The settings effect depends on t, so it has to keep its identity across renders.
  const t = (key: string) => key;
  return { useTranslation: () => ({ t }) };
});
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const MOD_A = "C:\\mods\\A";
const MOD_B = "C:\\mods\\B";
const LOADING = "page.tools.texture_resizer.loading_textures";

const SETTINGS: TextureResizeSettings = {
  mode: "custom",
  operation: "resize",
  percent: 50,
  customWidth: 2048,
  customHeight: 2048,
  outputFormat: "",
  backup: true,
  upscaleScale: 2,
  upscaleModel: "realesr-animevideov3",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function textureItem(fileName: string): TextureResizeListItem {
  return {
    filePath: fileName,
    relativePath: fileName,
    fileName,
    fileSize: 1,
    format: "png",
    colorSpace: "srgb",
    layerCount: 1,
    mipLevelCount: 1,
    originalWidth: 64,
    originalHeight: 64,
    targetWidth: 64,
    targetHeight: 64,
    canResize: true,
    canUpscale: true,
    canConvertFormat: true,
    canProcess: true,
    availableOutputFormats: ["png"],
    outputFormatDefault: "png",
  };
}

afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

describe("TextureResizerWorkspace load ordering", () => {
  it("drops the settings response of a mod that was switched away from", async () => {
    const settingsA = deferred<TextureResizeSettings>();
    const settingsB = deferred<TextureResizeSettings>();
    vi.mocked(Tools.GetTextureResizeSettings)
      .mockReturnValueOnce(settingsA.promise as never)
      .mockReturnValueOnce(settingsB.promise as never);
    vi.mocked(Tools.ListTextureMod).mockResolvedValue([textureItem("b.png")] as never);

    const view = render(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_A} />);
    view.rerender(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_B} />);

    await act(async () => settingsA.resolve({ ...SETTINGS, percent: 25 }));
    expect(Tools.ListTextureMod).not.toHaveBeenCalled();

    await act(async () => settingsB.resolve({ ...SETTINGS, percent: 75 }));
    expect(Tools.ListTextureMod).toHaveBeenCalledOnce();
    expect(Tools.ListTextureMod).toHaveBeenCalledWith(MOD_B, { ...SETTINGS, percent: 75 });
    expect(screen.getByText("b.png")).toBeTruthy();
  });

  it("keeps loading until the current list arrives when the previous one resolves first", async () => {
    const listA = deferred<TextureResizeListItem[]>();
    const listB = deferred<TextureResizeListItem[]>();
    vi.mocked(Tools.GetTextureResizeSettings).mockResolvedValue(SETTINGS as never);
    vi.mocked(Tools.ListTextureMod).mockImplementation(
      (path) => (path === MOD_A ? listA.promise : listB.promise) as never,
    );

    const view = render(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_A} />);
    await act(async () => {});
    view.rerender(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_B} />);
    await act(async () => {});
    expect(Tools.ListTextureMod).toHaveBeenCalledTimes(2);

    await act(async () => listA.resolve([textureItem("a.png")]));
    expect(screen.queryByText("a.png")).toBeNull();
    expect(screen.getByText(LOADING)).toBeTruthy();

    await act(async () => listB.resolve([textureItem("b.png")]));
    expect(screen.getByText("b.png")).toBeTruthy();
    expect(screen.getByText(MOD_B)).toBeTruthy();
  });

  it("ignores a list for the previous mod that arrives after the current one", async () => {
    const listA = deferred<TextureResizeListItem[]>();
    const listB = deferred<TextureResizeListItem[]>();
    vi.mocked(Tools.GetTextureResizeSettings).mockResolvedValue(SETTINGS as never);
    vi.mocked(Tools.ListTextureMod).mockImplementation(
      (path) => (path === MOD_A ? listA.promise : listB.promise) as never,
    );

    const view = render(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_A} />);
    await act(async () => {});
    view.rerender(<TextureResizerWorkspace mode="mod" fixedTargetPath={MOD_B} />);
    await act(async () => {});

    await act(async () => listB.resolve([textureItem("b.png")]));
    await act(async () => listA.resolve([textureItem("a.png")]));
    expect(screen.queryByText("a.png")).toBeNull();
    expect(screen.getByText("b.png")).toBeTruthy();
    expect(screen.getByText(MOD_B)).toBeTruthy();
    expect(screen.queryByText(MOD_A)).toBeNull();
  });
});
