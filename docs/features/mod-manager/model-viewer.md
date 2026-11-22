# Model Viewer

The Model Viewer shows a mod's model, textures, and toggle states without launching the game.

::: warning
Some mods may fail to open in the viewer, or their models or textures may not display correctly.
:::

## Supported Games

- Genshin Impact (GIMI)
- Honkai: Star Rail (SRMI)
- Zenless Zone Zero (ZZMI)
- Wuthering Waves (WWMI, RabbitFX)
- Arknights: Endfield (EFMI)

NTE mods are not supported.

## Open the Model Viewer

- Right-click a mod and select `Model Viewer` in the `Tools` section.
- In grid view, click the `Open in model viewer` button (cube icon) on a mod card.
- In grid view, right-click a [model preview](/features/mod-manager/previews#model-preview) and select `Open in model viewer`.
- In Windows File Explorer, right-click a mod folder and select `Open in Model Viewer`. This item appears only in the installed version, and opens the Model Viewer in its own window.
- Select `Open in model viewer` in [Blender Live Preview](/features/mod-manager/blender-live-preview).

## Controls

- **Rotate**: Drag with the left mouse button.
- **Pan**: Drag with the right or middle mouse button, or drag with the left button while holding `Shift`.
- **Zoom**: Use the mouse wheel.
- **Axis views**: Click an axis end in the axis indicator at the top right of the viewport to view the model along that axis.
- **Forms**: Mods with more than one form show a `Forms` list at the top left of the viewport. Select a form to show only that form, or `All` to show every form.

## Menu Bar

### Model

- `Rotate`: Rotates the model with `Left 90°`, `Right 90°`, `Up 90°`, `Down 90°`, and `Flip 180°`.
- `Reset`: Returns the model's rotation and the camera to their starting state.
- `Set Character…` assigns a character to the current mod. Use `Clear Mod Assignment` to return to the folder's character.
- `Character Motion`, `Original Game Data`, `Missing Parts`, `Expression`: Shown when the assigned character and model support them. See [Character Data](/features/mod-manager/character-data).

### Texture

- `Double Sided`: Renders both sides of each polygon.

### Rendering

- `Tone Mapping`: Selects `Neutral`, `ACES Filmic`, or `None`.
- `Environment`: Selects `Studio`, `Soft`, or `None`.
- `Exposure`: Changes the exposure from 0 to 4 with the `-0.1` and `+0.1` buttons or the input field. `Reset` restores the default.
- `Toon Shadows`: Turns toon shading on or off.
- Toon style submenu: Named `Genshin Toon Style`, `Star Rail Toon Style`, `Zenless Zone Zero Toon Style`, `Wuthering Waves Toon Style`, or `Toon Style` (Endfield) depending on the game. See [Toon Style](#toon-style).
- `Normal Maps`: Turns normal maps on or off for mods that have them.

### Toggle

Shown for mods with toggles.

- `Save to INI`: Writes the current toggle values to the mod's INI files. When a game that loaded the mod is running and connected to the app, the values are also applied to the game.
- `Reset`: Restores the defaults from the INI files.
- `Save as preset` / `Apply preset`: Saves the current values as a [toggle preset](/features/mod-manager/presets#toggle-presets) or applies a saved one. Applying a preset writes it to the INI files and also applies it to a connected game.
- `Preview hovered values in game`: Shows the value under the cursor in the Toggle Viewer in the running game.
- `Show Overlay in Viewer`: Shows the mod's in-game toggle overlay inside the viewer. When it is off, the overlay is shown at the top of the Toggle Viewer.
- `Reset Overlay Position and Size`: Returns the overlay in the viewer to its starting position and size.

Drag the overlay in the viewer to move it, and resize it with `Ctrl` + mouse wheel or its bottom-right corner.

### Misc

- `Capture and set preview`: Saves the center of the viewport as a square `preview.png` with a transparent background in the mod folder. If the mod already has a preview, a confirmation is shown before it is replaced.
- `Save as PNG`: Saves the view as a PNG file with a transparent background at 1x, 2x, or 4x the viewport size. Each submenu item shows the resulting size, and the file location is chosen in the Save dialog.

## Toon Style {#toon-style}

The toon style submenu in the `Rendering` menu changes these items.

- `Outlines`, `Self Shadow`, `Light Follows Camera`
- `Flat Face Shading`: Shown for Endfield only.
- `Shading`: Adjusts per-game shading values such as `Shadow Threshold`, `Shadow Brightness`, and `Outline Color`. `Reset Shading` restores the defaults.
- Genshin Impact also shows:
  - `Genshin Texture Folder...`: Selects a folder that contains Genshin toon textures. `Clear Texture Folder` removes it.
  - `Shadow Ramp`: Selects `Auto`, `Built-in`, or a ramp in the texture folder.
  - `Face Shadow Map`: When a face is drawn, selects `Auto`, `Boy`, `Girl`, `Lady`, `Loli`, or `Male`.

Mods with an assigned character use that character's toon style, and changes are saved per character. Other mods save changes per game. See [Character Data](/features/mod-manager/character-data#toon-style).

## Toggle Viewer

For mods with toggles, the Toggle Viewer on the right lists each toggle. Changing a value redraws the model in that state, and hovering a value previews the model with it.

- Values that have no effect in the current combination are disabled. Hovering one shows the variables that must change with it, and `Apply this combination` changes them together.
- Values that only change textures or effects the viewer does not render are listed under `Not shown in viewer`. They are still saved in presets.
- Drag the splitter to resize the panel, or click it to show or hide the panel. While the splitter has focus, `Enter` or `Space` shows or hides the panel, and `←`/`→` resize it.

## Playback Controller {#playback}

When a mod has animations or [character motion](/features/mod-manager/character-data#character-motion) is playing, a playback controller appears at the bottom of the viewport. The character data download status and the [texture source](#texture-choice) item are also shown on this controller.

- Mod animations and character motion each have their own lane. Each lane has `Play`/`Pause`, a clip menu, `Go to start`, `Go to end`, and the playback speed (0.25×–2×).
- Drag the frame slider to move to a frame. While the slider has focus, `←`/`→` step one frame, and `Home`/`End` jump to the start and end.
- Turn on `Interpolate frames` in the speed menu to interpolate between frames for smoother playback.
- Drag the grip on the left of the controller (`Move controller`) to move it. Double-click the grip, or press `Home` while it has focus, to return it to its default position.

## Texture Source {#texture-choice}

When a mod's INI replaces textures by hash, the viewer cannot tell which texture some parts use, and those parts are drawn with in-game textures. In that case, the playback controller shows `N parts use in-game textures`.

Click it to show these items.

- `Highlight`: Highlights the parts that use in-game textures.
- `Texture source`: For each part, selects `In-game texture` or a texture from another part of the same model. The choice is saved in `nhd.json` in the mod folder and is also used for model previews in grid view.

## Game Link

When the mod's importer uses the built-in launcher and the myparsleycat XXMI libraries, `Save as preset`, `Apply preset`, and `Preview hovered values in game` are available. While a game that loaded the mod is running, `Apply to game` at the top of the Toggle Viewer applies the current values to the game.
