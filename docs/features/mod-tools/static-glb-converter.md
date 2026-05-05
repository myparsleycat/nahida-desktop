# Static GLB Converter / Model Viewer

This tool converts mods into static GLB files. You can open the converted GLB in Blender or an online GLB viewer to inspect the model. The model viewer is built on top of the static GLB converter.

::: info
The static GLB converter and model viewer currently support only `Genshin Impact`, `Honkai: Star Rail`, `Zenless Zone Zero`, and `Wuthering Waves`.  
Some mods, especially many animation mods, may not convert correctly.
:::

## Options

You can configure the following options on the tool screen:

- **Asset Layout Path**:
  Specifies the 3DMigoto asset path. This option is saved permanently when changed. See [Assets](#assets) for details.

- **Target Mod Path**:
  Specifies the path of the mod to convert.

- **Output GLB Path**:
  Selects where the converted GLB model files will be saved.

- **Texture Format**:
  Selects the texture image format.  
  For faster conversion and smaller GLB size, use `JPEG (Alpha Safe)`.

## Conversion

After conversion, folders and files like the following are created in the output path:

```text
output/
├─ glb/
│  └─ converted GLB files
├─ ui/
│  └─ toggle viewer asset files
└─ manifest.json
```

You can find the converted GLB files inside the `glb` folder.

## Assets

For `Genshin Impact`, `Honkai: Star Rail`, and `Zenless Zone Zero`, assets are required to generate GLB files. The official asset repositories for each game are listed below:

- Genshin Impact: [SilentNightSound/GI-Model-Importer-Assets](https://github.com/SilentNightSound/GI-Model-Importer-Assets)
- Honkai: Star Rail: [SilentNightSound/SR-Model-Importer-Assets](https://github.com/SilentNightSound/SR-Model-Importer-Assets)
- Zenless Zone Zero: [leotorrez/ZZ-Model-Importer-Assets](https://github.com/leotorrez/ZZ-Model-Importer-Assets)
- Wuthering Waves: [SpectrumQT/WWMI-Assets](https://github.com/SpectrumQT/WWMI-Assets)

Download the assets for the games you need and organize them like this:

```text
assets/
├─ GI-Model-Importer-Assets/
├─ SR-Model-Importer-Assets/
├─ ZZ-Model-Importer-Assets/
└─ WWMI-Assets/
```

Then set the `assets` folder as the **Asset Layout Path**.

::: tip
Each repository may not always reflect the latest live game state. If assets for a specific character or weapon are missing, or the data is outdated, you can dump the assets yourself with tools such as [gui_collect](https://github.com/Petrascyll/gui_collect).
:::

::: tip
Among the asset files, only `vb`, `ib`, `fmt`, `json`, and `txt` files are required for GLB conversion. If you use the assets only for GLB conversion, you can safely delete the large `dds` files to save disk space.
:::
