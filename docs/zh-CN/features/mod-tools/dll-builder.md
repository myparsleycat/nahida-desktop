# 4001 Fixer

《原神》的 `4001` 错误（检测到非法程序）通常可以通过使用从 `XXMI-Libs-Package` 源码构建的 DLL 替代 XXMI 自带的默认 DLL 来解决。

`4001 Fixer` 可以构建用于替换的 `d3d11.dll`，或对现有 DLL 进行 PE 填充多样化处理。

## 安装必需的构建工具

构建工具需要手动安装。无需另行安装 Git：Nahida Desktop 会在首次构建时下载便携版 Git（MinGit，约 40MB），并在应用数据文件夹中使用。

1. 下载并运行 [Visual Studio Build Tools 2022](https://aka.ms/vs/17/release/vs_BuildTools.exe)。
2. 在单独组件中选择以下项目后进行安装：
   - **MSVC v143 - VS 2022 C++ x64/x86 生成工具（最新）**
   - **Windows 11 SDK (10.0.22621.0)**

::: warning
构建工具大约需要 4.4 GB 的磁盘空间。
:::

## 连接 XXMI

使用此功能前，需要先完成 XXMI 设置。详情请参阅[连接 XXMI](/zh-CN/others/set-up-xxmi)。

## 构建 DLL

构建页面包含以下选项：

- **提供方**：选择要构建的 `XXMI-Libs-Package` 仓库。`myparsleycat` 会构建 `XXMI-Libs-Package-Forked` 分支仓库。
- **版本**：选择要构建的 XXMI 库版本。
- **目标 GIMI Importer**：选择应用该 DLL 的 GIMI Importer。`4001` 错误只出现在《原神》中，因此只列出 GIMI Importer。

点击 **开始构建** 按钮开始构建。如果因找不到 Windows SDK 头文件（如 `crtdbg.h`）或所需的 SDK 版本而构建失败，会显示安装或修复指引。如果已安装 SDK，请在 Visual Studio Installer 中修复对应的安装，或在 **Visual Studio 路径** 中指定 Visual Studio 或 Build Tools 2022 的安装文件夹或其 `vcvars64.bat`。

## 多样化 DLL 填充

**多样化 DLL 填充** 选项卡会在不改变 DLL 布局的前提下，对所选 GIMI Importer 的 `d3d11.dll` 的 PE 填充字节进行多样化处理。处理前会在同一文件夹中创建 `pepd` 备份文件；多样化后的 DLL 生效期间会显示恢复操作。

## 系统文件夹与管理员权限

如果 GIMI Importer 位于 `C:\Program Files\` 等受保护的位置，安装 DLL 时 Windows 会显示 UAC 提示。拒绝提示会导致操作失败。如果游戏正在使用 `d3d11.dll`，则无法替换该文件，请先关闭游戏。

## 不安全模式

未开启 **不安全模式** 时，XXMI 会对非官方提供的 DLL 显示警告。通过 4001 Fixer 应用 DLL 时，会自动为 GIMI Importer 开启不安全模式。如果仍看到 `d3d11.dll` 签名无效的警告，请手动开启：

- **内置启动器**：在 XXMI 页面中打开 GIMI Importer，然后在 `高级` 选项卡中开启 `不安全模式`。
- **XXMI Launcher**：在 XXMI Launcher 设置中开启 Unsafe Mode。

然后重新启动游戏。如果要在内置启动器中恢复为已签名的 DLL，请点击该 Importer `软件包` 选项卡中的 `恢复官方 DLL`。
