# 静态 GLB 转换器 / 模型查看器

这个工具可以把模组转换为静态 GLB 文件。转换后的 GLB 可以在 Blender 或在线 GLB 查看器中打开，以检查模型内容。模型查看器本身也是基于静态 GLB 转换功能实现的。

::: info
静态 GLB 转换器目前支持 `原神`、`崩坏：星穹铁道`、`绝区零`和`鸣潮`；模型查看器还支持 `明日方舟：终末地`。
此外，部分模组以及大多数动画类模组可能无法被正确转换。
:::

## 选项

在工具页面中，可以设置以下选项：

- **Asset Layout Path**：
  指定 3DMigoto 资源路径。此选项在修改后会被永久保存。详情请参考 [资源](#资源)。

- **Target Mod Path**：
  指定需要转换的目标模组路径。

- **Output GLB Path**：
  选择转换后 GLB 模型文件的保存位置。

- **Texture Format**：
  选择纹理图片格式。  
  为了兼顾转换速度和 GLB 体积，建议使用 `JPEG (Alpha Safe)` 模式。

## 转换

完成转换后，输出路径中会生成如下目录和文件：

```text
output/
├─ glb/
│  └─ 已转换的 GLB 文件
├─ ui/
│  └─ 切换查看器资源文件
└─ manifest.json
```

转换后的 GLB 文件可以在 `glb` 文件夹中找到。

## 资源

对于 `原神`、`崩坏：星穹铁道`、`绝区零`，生成 GLB 需要对应的资源文件。各游戏的官方资源仓库如下：

- 原神: [SilentNightSound/GI-Model-Importer-Assets](https://github.com/SilentNightSound/GI-Model-Importer-Assets)
- 崩坏：星穹铁道: [SilentNightSound/SR-Model-Importer-Assets](https://github.com/SilentNightSound/SR-Model-Importer-Assets)
- 绝区零: [leotorrez/ZZ-Model-Importer-Assets](https://github.com/leotorrez/ZZ-Model-Importer-Assets)
- 鸣潮: [SpectrumQT/WWMI-Assets](https://github.com/SpectrumQT/WWMI-Assets)

下载所需游戏的资源后，请按如下方式组织目录：

```text
assets/
├─ GI-Model-Importer-Assets/
├─ SR-Model-Importer-Assets/
├─ ZZ-Model-Importer-Assets/
└─ WWMI-Assets/
```

随后将 `assets` 文件夹设置为 **Asset Layout Path**。

::: tip
这些仓库不一定始终与游戏的最新版本保持同步。如果某个角色或武器的资源缺失，或者数据已经过期，可以借助 [gui_collect](https://github.com/Petrascyll/gui_collect) 等工具自行导出资源。
:::

::: tip
在资源文件中，GLB 转换真正需要的只有 `vb`、`ib`、`fmt`、`json`、`txt` 文件。如果这些资源仅用于 GLB 转换，可以删除占用空间较大的 `dds` 文件。
:::
