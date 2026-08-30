# 快速开始

## 从 GitHub Releases 下载

最新版本提供以下两种 Windows amd64 可执行文件：

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe)：需要安装后使用的安装程序版本
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe)：无需安装即可运行的便携版本

版本变更和其他发布资源可以在 [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) 页面查看。

## 从源码构建

开始构建前，请先安装以下工具：

- Go（`go.mod` 中指定的版本）
- Node.js 22
- pnpm v11
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` 会安装并构建前端依赖、生成 Wails 绑定，然后在 `bin/nahida-desktop.exe` 创建 Windows 可执行文件。项目通过 `go tool wails3` 运行 `go.mod` 中固定的 Wails v3 CLI 版本，因此无需另行安装 Wails CLI。
