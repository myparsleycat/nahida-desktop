# 连接 XXMI

导航栏中的 XXMI 页面用于选择游戏的启动方式并管理 XXMI Importer。在模组管理器中添加 XXMI 游戏或使用需要 XXMI 的工具之前，请先完成设置。

## 选择游戏启动器

在 XXMI 页面顶部选择 `游戏启动器`：

- **内置**：由 Nahida Desktop 自行安装和更新 Importer 包与 XXMI 库，并启动游戏。
- **XXMI Launcher**：Nahida Desktop 使用已安装的 XXMI Launcher。

切换时，两种方式的设置都会保留。

## 内置启动器

1. 设置 `导入器根目录`。每个 Importer 都会安装在此目录下各自的子文件夹中。
2. 如果在尚未配置任何 Importer 时找到已安装的 XXMI Launcher，可以导入其设置。软件包和 XXMI 库会重新安装，你可以选择让 `Mods` 和 `d3dx_user.ini` 保留在 XXMI Launcher 文件夹中，还是移动到新的 Importer 文件夹。
3. 在侧边栏中选择 Importer，在 `常规` 选项卡中设置其游戏文件夹，然后在 `软件包` 选项卡中安装其软件包。

每个 Importer 页面包含以下选项卡：

- **常规**：游戏文件夹、模组文件夹、Importer 图标、游戏启动方式（直接启动、Steam、Epic Games 或自定义命令）以及 DLL 注入选项。
- **软件包**：Importer 包和 XXMI 库的版本。库提供方可以是 SpectrumQT 或 myparsleycat 分支。
- **游戏设置**（仅 GIMI、SRMI、HIMI 和 WWMI）：FPS 解锁、HDR、纹理精度以及其他各游戏专属选项。《原神》使用 GI FPS Unlocker。
- **高级**：3DMigoto 选项，例如捕获模式、不安全模式、日志级别、模组按键绑定和自定义命令。
- **d3dx.ini**：编辑 `d3dx.ini` 设置。
- **工具**：创建 Quick Start 桌面快捷方式、修复运行时或软件包、优化模组 INI 文件，以及开启日志模式以收集用于问题反馈的日志和崩溃转储。
- **运行时**（仅限有运行时的 Importer）：由工具安装到游戏 Mods 文件夹中的全局模组，启动时会自动更新。

`ReShade` 页面用于安装 ReShade 及其官方效果包，并设置共享的预设文件夹。

`管理` 显示待处理的更新、全局选项、启动前检查和缓存清理，并可重置内置启动器。

### DLL 注入器

Importer 页面 `常规` 选项卡中的 `DLL 注入器` 用于选择加载运行时 DLL 的方式：

- `现有启动器方式`：默认值。由运行时自身的钩子或加载器加载。
- `Native`：不使用钩子，而是将运行时和额外 DLL 直接注入 64 位游戏。

`注入方式` 为 `Bypass` 时，仅注入额外 DLL。

`Native` 不需要 XXMI 加载器（`3dmloader.dll`）或旧版 3DMigoto 加载器，因此旧版运行时也可以在没有 XXMI 库的情况下注入额外 DLL。它仅适用于 64 位游戏，并会在 `游戏启动超时（秒）` 内等待游戏进程和每个 DLL 加载完成。

## XXMI Launcher

1. 在 `XXMI路径` 中输入 XXMI Launcher 文件夹，然后点击 `保存`。如果你没有修改安装位置，通常为：

   ```txt
   %AppData%\XXMI Launcher
   ```

2. 如果不知道安装位置，请点击 `自动扫描`。

::: warning
`自动扫描` 会在所有驱动器中查找 XXMI Launcher。如果存在多个安装，可能会选中其他安装，请确认检测到的路径。
:::

保存路径后，页面会显示已启用的 Importer，并可更改 XXMI DLL 和 Importer 包的版本。

## 启动前检查

两种启动器都可以在游戏启动前检查以下问题：《原神》的动态角色分辨率、NVIDIA 平滑运动、3DMigoto 日志记录以及未压缩的纹理。

## 需要 XXMI 的功能

- 从模组管理器启动 XXMI 游戏（《异环》无需 XXMI）
- [4001 Fixer](/zh-CN/features/mod-tools/dll-builder)
- [持久保存切换状态](/zh-CN/features/mod-tools/persist-toggles)
- Toggle Viewer 生成器
