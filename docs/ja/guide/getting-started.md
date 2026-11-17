# はじめに

## GitHub Releases からダウンロード

最新バージョンは、次の 2 種類の Windows amd64 実行ファイルとして配布されています。

- [**nahida-desktop-windows-amd64-installer.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64-installer.exe): インストーラー版
- [**nahida-desktop-windows-amd64.exe**](https://github.com/myparsleycat/nahida-desktop/releases/latest/download/nahida-desktop-windows-amd64.exe): インストールせずに実行できるポータブル版

リリースノートとその他のファイルは [GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) ページにあります。

リリースページの `SHA256SUMS` には、両方のファイルのチェックサムが記載されています。Nahida Desktop は 64 ビット版 Windows でのみ動作します。GameBanana へのサインインには Microsoft Edge WebView2 Runtime を使用します。インストーラー版は自動的にインストールしますが、ポータブル版ではあらかじめインストールされている必要があります。

## ソースからビルド

ビルドには次のツールが必要です。

- Go（`go.mod` に指定されたバージョン）
- Task v3

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
go install github.com/go-task/task/v3/cmd/task@v3.53.1
task build
```

`task build` は Windows 実行ファイルを `bin/nahida-desktop.exe` に作成します。
