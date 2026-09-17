// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { useGridModelPreview } from "./grid-model-preview";
import { ModPreviewContainer } from "./mod-preview-container";

const modelPreviewHook = vi.hoisted(() => vi.fn());

vi.mock("@bindings/platform", () => ({ Shell: { OpenExternal: vi.fn() } }));
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
    <div onClick={parentClick}>
      <ModPreviewContainer
        mod={mod}
        modelPreviewEligible
        onDeletePreview={vi.fn()}
        onOpenModelViewer={openModelViewer}
        onPaste={paste}
      />
    </div>,
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

function renderPreview(value: typeof mod & { preview?: string }) {
  return render(
    <ModPreviewContainer
      mod={value}
      modelPreviewEligible
      onDeletePreview={vi.fn()}
      onOpenModelViewer={vi.fn()}
      onPaste={vi.fn()}
    />,
  );
}
