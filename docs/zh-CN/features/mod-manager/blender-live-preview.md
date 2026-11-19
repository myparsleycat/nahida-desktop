# Blender 实时预览

Blender 实时预览会在 [模型查看器](/zh-CN/features/mod-manager/model-viewer) 中实时显示正在 Blender 中编辑的 XXMI 模组，无需导出。支持原神 (GIMI)、崩坏：星穹铁道 (SRMI) 和绝区零 (ZZMI) 模组。

在工具界面中选择 `Blender 实时预览 (测试版)` 打开。

::: info
需要 Blender 3.6 或更高版本以及 Nahida Live Link 插件。
:::

## 安装插件

`插件（内置版本 X）` 中会列出在电脑上找到的 Blender 3.6 或更高版本的安装，以及每个 Blender 的插件状态。列表不是最新时，请点击 `刷新`。

- `安装`、`更新`、`重新安装`：安装应用内置版本的插件，并在 Blender 中启用。安装成功后，Blender 实时预览会开启。
- `移除`：移除插件。

以下情况无法安装：

- 该版本的 Blender 正在运行：关闭 Blender 后重试。
- 其他位置安装了另一份插件：先在 Blender 中将其移除。
- 插件文件夹链接到其他文件夹：在链接的文件夹中更新。

找不到 Blender 可执行文件时，只会安装插件文件。请在 Blender 的“编辑 > 偏好设置 > 插件”中启用 Nahida Live Link。

## 连接

1. 使用界面顶部的开关开启 Blender 实时预览。
2. 在 Blender 中启用 Nahida Live Link 插件后，Blender 会显示在 `已连接的 Blender` 列表中。
3. 为已连接的 Blender 设置以下项目，然后点击 `在模型查看器中打开`。
   - `游戏`：选择模组的游戏。场景中能识别游戏时会自动设置。
   - `用于游戏动作的角色`：选择用于 [角色数据](/zh-CN/features/mod-manager/character-data) 的角色。会根据对象名称推测并预先选择角色。

已连接的插件比应用内置版本旧时，会显示警告和更新按钮。

## 实时反映

模型查看器打开期间，在 Blender 中编辑的网格和贴图会立即反映到查看器中。添加对象等导致结构变化时，会重新加载场景。
