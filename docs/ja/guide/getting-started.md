# はじめに

## GitHub Releases からダウンロード

[GitHub Releases](https://github.com/myparsleycat/nahida-desktop/releases/latest) ページで `Nahida-Desktop-Setup-x.x.x.exe` をクリックすると、最新バージョンのインストーラーをダウンロードできます。

## ソースからビルド

ビルド前に、次のツールがインストールされている必要があります。

- pnpm v10
- Rust ツールチェーン（`cargo`, `rustc` を含む）

```sh
git clone https://github.com/myparsleycat/nahida-desktop.git
cd nahida-desktop
pnpm install && pnpm run build:native
pnpm run build:win
```
