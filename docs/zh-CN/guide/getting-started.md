# 快速开始

## 从 GitHub Releases 下载

打开 [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 页面，点击 `Nahida-Desktop-Setup-<version>.exe` 即可下载最新版本的安装程序。

## 从源码构建

开始构建前，请先安装以下工具：

- pnpm v10
- Rust 工具链（包含 `cargo` 和 `rustc`）

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
pnpm install && pnpm run build:native
pnpm run build:win
```
