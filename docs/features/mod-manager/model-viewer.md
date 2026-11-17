# Model Viewer

The Model Viewer shows a mod's model, textures, and toggle states without launching the game.

::: warning
Some mods may fail to open in the viewer, or their models or textures may be displayed incorrectly.
:::

## Supported Games

- `Genshin Impact` (GIMI)
- `Honkai: Star Rail` (SRMI)
- `Zenless Zone Zero` (ZZMI)
- `Wuthering Waves` (WWMI)
- `Arknights: Endfield` (EFMI)

## Open the Model Viewer

Right-click a mod in the Mod Manager and select `Model Viewer`, or click the `Open in model viewer` button (cube icon) on a mod card in the grid layout.

## Menu Bar

- **Model**: Rotates the model and resets the model or camera.
- **Texture**: `Double Sided` renders both faces of each polygon.
- **Rendering**: Changes tone mapping, environment, and exposure, and turns normal maps on or off. For supported games, `Toon Shadows` and the game's toon style submenu change the toon shading, including outlines and self shadow.
- **Toggle**: Shown for mods with toggles.
  - `Save to INI`: Writes the current toggle values to the mod's INI file.
  - `Reset`: Restores the default values from the INI file.
  - `Save as preset` / `Apply preset`: Saves the current values as a [toggle preset](/features/mod-manager/#toggle-presets) or applies one.
  - `Preview hovered values in game`: Shows the value under the cursor in the running game.
  - `Show Overlay in Viewer`: Shows the mod's in-game toggle overlay inside the viewer. Drag it to move it, and resize it with `Ctrl` + mouse wheel or its bottom-right corner.
- **Misc**:
  - `Capture and set preview`: Saves the viewport as a square `preview.png` in the mod folder. If the mod already has a preview, the viewer asks before replacing it.

## Game Link

`Save as preset`, `Apply preset`, and `Preview hovered values in game` are available when the mod's importer uses the built-in launcher with the myparsleycat XXMI libraries. While such a game is running, `Apply to game` at the top of the Toggle Viewer applies the current values to the game.

## Toggle Viewer

For mods with toggles, the Toggle Viewer on the right lists each toggle. When a value changes, the model is drawn again in that state. Drag the splitter to resize the panel, or click it to show or hide the panel.

Values that change only textures or effects the viewer does not draw are marked `Not shown in viewer`; they are still saved in presets.

## Animation Playback

If the mod includes animations, a playback bar is shown at the bottom of the viewer. It plays or pauses the animation, selects a clip, changes the playback speed, and seeks to a frame with the slider.
