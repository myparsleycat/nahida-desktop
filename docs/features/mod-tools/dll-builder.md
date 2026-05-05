# d3d11.dll Builder

In Genshin Impact, the illegal program detection error `4001` continues to be reported.
Previously, this could often be handled by applying cloud provider flags and similar workarounds, but recently more cases have appeared where older methods no longer solve the issue.

In Korean communities, many users have confirmed that the `4001` issue can often be resolved by cloning the `XXMI-Libs-Package` repository directly and using a self-built DLL instead of the default DLL provided by XXMI.

The `d3d11.dll Builder` automates this process to make it easier.

## Install Required Build Tools

This feature automates much of the DLL build process, but you still need to install the required build tools yourself.

1. Visit the [Visual Studio download page](https://visualstudio.microsoft.com/downloads/), download **Visual Studio Community**, and install it.
2. In Visual Studio Installer, select the **Desktop development with C++** workload.
3. In individual components, verify that the following items are included, then complete the installation.
   - **MSVC build tools for x64/x86 (latest)**
   - **Windows 11 SDK**
   - **MSVC v143 - VS 2022 C++ x64/x86 build tools (v14.44-17.14)**

::: warning
Installing the build tools may require more than 10 GB of disk space. Make sure you have enough free space before proceeding.
:::

## Configure the XXMI Path

To use this feature, you need to configure the XXMI path first. See [Set Up XXMI](/others/set-up-xxmi) for details.

## Build the DLL

![Image](/features/mod-tools/dll-builder/page.png)

On the build screen, you can configure the following options:

- **Provider**:
  Select the provider of the `XXMI-Libs-Package` repository to use for the build.

  ::: warning
  Do not select the repository from `myparsleycat`, because it is no longer maintained.
  :::

- **Version**:
  Select the XXMI library version to build.

- **Target Importer**:
  Select the importer that will use the built DLL.
  Since the `4001` error currently occurs only in Genshin Impact, choose **GIMI**.

After selecting all options, click **Start Build** to begin building the DLL.

## Unsafe Mode

![Image](/features/mod-tools/dll-builder/unsafe.png)

The XXMI Launcher shows a warning when you use an unverified DLL instead of an officially provided one.

When you build a DLL through the DLL Builder, **Unsafe Mode** is enabled automatically. However, if automatic activation does not apply correctly, you may still see a warning that the DLL signature is invalid.

If you see a warning that the `d3d11.dll` signature is invalid, enable **Unsafe Mode** manually in the XXMI Launcher settings.

After enabling Unsafe Mode, run the importer again to use the DLL you built.
