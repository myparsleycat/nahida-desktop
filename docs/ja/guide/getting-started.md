# はじめに

## GitHub Releases からダウンロード

最新バージョンは、次の 2 種類の Windows amd64 実行ファイルとして配布されています。

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): インストールして使用するインストーラー版
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): インストールせずに実行できるポータブル版

変更内容やその他のリリースアセットは、[GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) ページで確認できます。

## ソースからビルド

ビルド前に、次のツールがインストールされている必要があります。

- Go（`go.mod` に指定されたバージョン）
- Node.js 22
- pnpm v11
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` は、フロントエンド依存関係のインストールとビルド、Wails バインディングの生成を行い、Windows 実行ファイルを `bin/nahida-desktop.exe` に作成します。Wails v3 CLI は `go.mod` に固定されたバージョンを `go tool wails3` で実行するため、別途インストールする必要はありません。
