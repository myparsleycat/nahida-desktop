# Previews

The Mod Manager shows image or video previews for mods and for sidebar folders.

Supported file types are `png`, `jpg`, `jpeg`, `gif`, `webp`, `bmp`, `avif`, `avifs`, `mp4`, `webm`, `avi`, `mkv`, and `mov`. Videos play muted and loop.

## Mod Previews

The Mod Manager looks for the preview file of a mod in this order:

1. Files directly in the mod folder
2. Files in subfolders
3. Files in disabled (`DISABLED`) subfolders

When a location has several files, a file whose name starts with `preview` is chosen first, then a file whose name contains `preview`, then any other file. Case is ignored. Files whose names contain `normal`, `light`, `material`, or `diffuse` are treated as textures and skipped.

When a mod has several images, the preview in grid view shows the `Previous preview image` and `Next preview image` buttons. The selected image is saved in `nhd.json` in the mod folder and is shown next time.

## Folder Previews

A sidebar folder's preview uses an image or video file directly in that folder. With the sidebar shown as `Preview grid`, folder previews are shown large.

::: info
When `Search for previews in subfolders` is enabled in [Mod Settings](/features/mod-manager/settings) and a folder has no preview, the mod folders inside it are searched as well.
:::

## View a Preview

- `View full size`: Shows the preview large over the app window. Right-click the preview in grid view to select it. In list view, click the thumbnail.
- `Open in viewer`: Opens the preview file in the program associated with it in Windows. Select it in the `Preview` section of the mod menu or in the preview's menu in grid view.

## Set or Replace a Preview

- **Mod**: Copy an image file, an image URL, or an image to the clipboard, then right-click the mod and select `Paste` in the `Preview` section. In grid view, a mod without a preview also shows a `Paste` button in the preview area. Video files cannot be pasted.
- **Sidebar folder**: Drag and drop one image or video file onto the folder in the sidebar.
- **Model Viewer**: In the [Model Viewer](/features/mod-manager/model-viewer), select `Misc` → `Capture and set preview`.

If a preview already exists, a confirmation is shown before it is replaced.

`Delete preview` in the `Preview` section of the mod menu moves the current preview file to the recycle bin.

## Model Previews {#model-preview}

When `Show model previews for mods without previews` is enabled in [Mod Settings](/features/mod-manager/settings#layout), mods without an image or video show a model image of their current toggle state in grid view. NTE mods do not show model previews.

Right-click a model preview to select `Open in model viewer` or `Paste`.
