// @vitest-environment jsdom
import { Mod } from "@bindings/mod";
import type { FolderGroup, ModInfo } from "@renderer/types/mod";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { useGridModelPreview } from "./grid-model-preview";
import { ModPreviewContainer } from "./mod-preview-container";

const modelPreviewHook = vi.hoisted(() => vi.fn());

vi.mock("@bindings/mod", () => ({ Mod: { SetDefaultPreview: vi.fn() } }));
vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("react-i18next", () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock("./grid-model-preview", () => ({ useGridModelPreview: modelPreviewHook }));
vi.mock("./preview", () => ({
  Preview: ({ path }: { path?: string }) => <div data-testid="media-preview">{path}</div>,
}));

const mod = {
  id: "mod",
  name: "Test model",
  path: "C:/Mods/Test",
  isEnabled: true,
  mtime: 1,
  size: 1,
  inis: [{ name: "Character.ini", path: "Character.ini", toggleKeys: [] }],
};

beforeEach(() => {
  vi.mocked(Mod.SetDefaultPreview).mockResolvedValue(undefined);
  modelPreviewHook.mockReturnValue([
    vi.fn(),
    true,
    { status: "ready", url: "blob:model-preview" },
  ] satisfies ReturnType<typeof useGridModelPreview>);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it.each(["preview.webp", "preview.avif", "preview.mov"])(
  "keeps supported media %s ahead of the generated model image",
  (preview) => {
    renderPreview({ ...mod, preview: `C:/Mods/Test/${preview}` });

    expect(screen.getByTestId("media-preview").textContent).toContain(preview);
    expect(screen.queryByAltText("Test model")).toBeNull();
  },
);

it("shows a non-interactive model image and the reduced context menu", async () => {
  const openModelViewer = vi.fn();
  const paste = vi.fn();
  const parentClick = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <div onClick={parentClick}>
        <ModPreviewContainer
          mod={mod}
          modelPreviewEligible
          onDeletePreview={vi.fn()}
          onOpenModelViewer={openModelViewer}
          onPaste={paste}
        />
      </div>
    </QueryClientProvider>,
  );

  const image = screen.getByAltText("Test model");
  expect(image.getAttribute("src")).toBe("blob:model-preview");
  expect(image.getAttribute("draggable")).toBe("false");
  expect(document.querySelector("canvas")).toBeNull();

  fireEvent.contextMenu(image);
  const items = await screen.findAllByRole("menuitem");
  expect(items).toHaveLength(2);
  expect(items[0].textContent).toContain("page.mod.context-menu.open-model-viewer");
  expect(items[1].textContent).toContain("page.mod.context-menu.paste-preview");
  expect(screen.queryByText("page.mod.context-menu.delete-preview")).toBeNull();

  fireEvent.click(items[0]);
  expect(openModelViewer).toHaveBeenCalledOnce();
  expect(parentClick).not.toHaveBeenCalled();
});

it("only shows navigation for multiple candidate images", () => {
  renderPreview({ ...mod, preview: "C:/Mods/Test/cover.jpg" });
  expect(screen.queryByRole("button", { name: "page.mod.next-preview" })).toBeNull();

  cleanup();
  renderPreview({
    ...mod,
    preview: "C:/Mods/Test/cover.jpg",
    previewImages: ["C:/Mods/Test/cover.jpg"],
  });
  expect(screen.queryByRole("button", { name: "page.mod.next-preview" })).toBeNull();

  cleanup();
  renderPreview({ ...mod, preview: "C:/Mods/Test/preview.mp4", previewImages: images });
  expect(screen.getByRole("button", { name: "page.mod.next-preview" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "page.mod.previous-preview" })).toBeTruthy();
});

it("wraps backward, persists the selection, and updates the group cache without toggling", async () => {
  const queryClient = new QueryClient();
  const parentClick = vi.fn();
  const value = { ...mod, preview: images[0], previewImages: images };
  queryClient.setQueryData<FolderGroup>(["modGroup", "C:/Mods"], {
    name: "group",
    path: "C:/Mods",
    mods: [value],
  });
  render(
    <QueryClientProvider client={queryClient}>
      <div onClick={parentClick}>
        <ModPreviewContainer
          mod={value}
          selectedGroupPath="C:/Mods"
          modelPreviewEligible
          onDeletePreview={vi.fn()}
          onOpenModelViewer={vi.fn()}
          onPaste={vi.fn()}
        />
      </div>
    </QueryClientProvider>,
  );

  fireEvent.click(screen.getByRole("button", { name: "page.mod.previous-preview" }));
  await waitFor(() => expect(Mod.SetDefaultPreview).toHaveBeenCalledWith(mod.path, images[1]));
  await waitFor(() =>
    expect(queryClient.getQueryData<FolderGroup>(["modGroup", "C:/Mods"])?.mods[0].preview).toBe(
      images[1],
    ),
  );
  expect(parentClick).not.toHaveBeenCalled();
});

it("wraps forward from the last image", async () => {
  renderPreview({ ...mod, preview: images[1], previewImages: images });
  fireEvent.click(screen.getByRole("button", { name: "page.mod.next-preview" }));
  await waitFor(() => expect(Mod.SetDefaultPreview).toHaveBeenCalledWith(mod.path, images[0]));
});

it("prevents concurrent changes and retains the prior preview on failure", async () => {
  let rejectSave: (error: Error) => void = () => {};
  vi.mocked(Mod.SetDefaultPreview).mockImplementation(
    () =>
      new Promise<void>((_, reject) => (rejectSave = reject)) as ReturnType<
        typeof Mod.SetDefaultPreview
      >,
  );
  renderPreview({ ...mod, preview: images[0], previewImages: images });

  fireEvent.click(screen.getByRole("button", { name: "page.mod.next-preview" }));
  expect(
    screen.getByRole("button", { name: "page.mod.previous-preview" }).hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "page.mod.previous-preview" }));
  expect(Mod.SetDefaultPreview).toHaveBeenCalledTimes(1);
  rejectSave(new Error("failed"));
  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith("page.mod.toast.change-preview-error"),
  );
  expect(screen.getByTestId("media-preview").textContent).toBe(images[0]);
});

const images = ["C:/Mods/Test/cover.jpg", "C:/Mods/Test/nested/shot.png"];

function renderPreview(value: ModInfo) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <ModPreviewContainer
        mod={value}
        modelPreviewEligible
        onDeletePreview={vi.fn()}
        onOpenModelViewer={vi.fn()}
        onPaste={vi.fn()}
      />
    </QueryClientProvider>,
  );
}
