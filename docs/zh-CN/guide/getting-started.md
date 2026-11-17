# 快速开始

## 从 GitHub Releases 下载

最新版本提供以下两种 Windows amd64 可执行文件：

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe)：安装程序版本
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe)：无需安装即可运行的便携版本

发行说明和其他文件位于 [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 页面。

发布页面中的 `SHA256SUMS` 列出了这两个文件的校验和。Nahida Desktop 仅支持 64 位 Windows。GameBanana 登录使用 Microsoft Edge WebView2 Runtime；安装程序版本会自动安装它，便携版本则需要事先安装。

## 从源码构建

构建需要以下工具：

- Go（`go.mod` 中指定的版本）
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` 会在 `bin/nahida-desktop.exe` 创建 Windows 可执行文件。
