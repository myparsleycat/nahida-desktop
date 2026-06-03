# d3d11.dll 构建器

在《原神》中，非法程序检测错误 `4001` 仍然在持续被报告。
过去有时可以通过应用云服务提供商标记等方式解决，但最近越来越多的情况表明，旧方法已经无法生效。

在韩国社区中，许多用户确认：与其使用 XXMI 默认提供的 DLL，不如直接克隆 `XXMI-Libs-Package` 仓库并自行构建 DLL，这样通常可以解决 `4001` 问题。

`d3d11.dll 构建器` 就是为了简化这一流程而提供的自动化功能。

## 安装必需的构建工具

此功能会自动处理 DLL 构建流程中的大部分步骤，但构建 DLL 所需的工具仍需由用户自行安装。

1. 打开 [Visual Studio 下载页面](https://visualstudio.microsoft.com/downloads/)，下载并安装 **Visual Studio Community**。
2. 在 Visual Studio Installer 中选择 **使用 C++ 的桌面开发** 工作负载。
3. 在单独组件中确认包含以下项目后再继续安装。
   - **适用于 x64/x86 的 MSVC 构建工具（最新）**
   - **Windows 11 SDK**
   - **MSVC v143 - VS 2022 C++ x64/x86 构建工具 (v14.44-17.14)**

::: warning
安装这些构建工具可能需要超过 10 GB 的磁盘空间。开始安装前，请确认磁盘剩余空间充足。
:::

## 配置 XXMI 路径

要使用此功能，必须先配置 XXMI 路径。详细说明请参考 [连接 XXMI](/zh-CN/others/set-up-xxmi)。

## 构建 DLL

![图片](/features/mod-tools/dll-builder/page.png)

在构建页面中，你可以设置以下选项：

- **Provider**：
  选择要用于构建的 `XXMI-Libs-Package` 仓库提供者。

- **Version**：
  选择要构建的 XXMI 库版本。

- **Target Importer**：
  选择要使用该 DLL 的 Importer。
  由于 `4001` 错误目前只出现在《原神》中，请选择 **GIMI**。

完成全部选项设置后，点击 **Start Build** 按钮即可开始构建 DLL。

## 非安全模式

![图片](/features/mod-tools/dll-builder/unsafe.png)

如果使用的不是 XXMI 官方提供的 DLL，而是未经验证的 DLL，XXMI Launcher 会显示警告信息。

通过 DLL 构建器构建时，**非安全模式** 会自动启用。但如果自动启用没有正确生效，仍可能看到 DLL 签名无效的警告。

如果你看到了 `d3d11.dll` 签名无效的警告，请在 XXMI Launcher 设置中手动启用 **非安全模式**。

启用非安全模式后，重新运行 Importer，即可使用你构建的 DLL。
