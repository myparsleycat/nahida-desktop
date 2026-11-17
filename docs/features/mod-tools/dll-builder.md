# 4001 Fixer

The `4001` error (illegal program detected) in Genshin Impact is often resolved by using a DLL built from the `XXMI-Libs-Package` source instead of the default DLL that ships with XXMI.

The `4001 Fixer` builds a replacement `d3d11.dll` or applies PE padding diversification to the existing DLL.

## Install Required Build Tools

The build tools must be installed manually. Git is not required: on the first build, Nahida Desktop downloads a portable Git (MinGit, about 40 MB) and uses it from its app data folder.

1. Download and run [Visual Studio Build Tools 2022](https://aka.ms/vs/17/release/vs_BuildTools.exe).
2. Under Individual components, select the following items and install them:
   - **MSVC v143 - VS 2022 C++ x64/x86 build tools (Latest)**
   - **Windows 11 SDK (10.0.22621.0)**

::: warning
The build tools require about 4.4 GB of disk space.
:::

## Set Up XXMI

This feature requires XXMI to be set up first. See [Set Up XXMI](/others/set-up-xxmi).

## Build the DLL

The build screen has the following options:

- **Provider**: The `XXMI-Libs-Package` repository to build. `myparsleycat` builds the `XXMI-Libs-Package-Forked` fork.
- **Version**: The XXMI library version to build.
- **Target GIMI Importer**: The GIMI importer that receives the built DLL. Only GIMI importers are listed because the `4001` error occurs only in Genshin Impact.

Click **Start Build** to start the build. If it fails because Windows SDK headers (such as `crtdbg.h`) or the required SDK version cannot be found, installation or repair guidance is shown. If the SDK is already installed, repair the installation in Visual Studio Installer, or set **Visual Studio path** to a Visual Studio or Build Tools 2022 install folder or its `vcvars64.bat`.

## Diversify DLL Padding

The **Diversify DLL Padding** tab diversifies the PE padding bytes of the selected GIMI importer's `d3d11.dll` without changing the DLL layout. It creates a `pepd` backup in the same folder first, and shows a restore action while the diversified DLL is in place.

## System Folders and Administrator Permission

If the GIMI importer is in a protected location such as `C:\Program Files\`, Windows shows a UAC prompt when the DLL is installed. Denying the prompt fails the operation. If the game is using `d3d11.dll`, the file cannot be replaced; close the game first.

## Unsafe Mode

Unless **Unsafe Mode** is on, XXMI shows a warning for DLLs that are not officially provided. Applying a DLL with the 4001 Fixer turns on Unsafe Mode for the GIMI importer. If a warning about an invalid `d3d11.dll` signature still appears, turn it on manually:

- **Built-in launcher**: On the XXMI page, open the GIMI importer and turn on `Unsafe mode` on the `Advanced` tab.
- **XXMI Launcher**: Turn on Unsafe Mode in the XXMI Launcher settings.

Then restart the game. To return to the signed DLL with the built-in launcher, click `Restore official DLL` on the importer's `Packages` tab.
