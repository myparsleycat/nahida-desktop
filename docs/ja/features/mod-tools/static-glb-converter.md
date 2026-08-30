# 静的 GLB 変換 / モデルビューア

このツールは MOD を静的 GLB に変換する機能を提供します。変換された GLB は Blender やオンライン GLB ビューアで開いてモデルを確認できます。モデルビューアは静的 GLB 変換機能を基盤として動作します。

::: info
静的 GLB 変換機能は現在 `原神`、`崩壊：スターレイル`、`ゼンレスゾーンゼロ`、`鳴潮` に対応し、モデルビューアは `アークナイツ：エンドフィールド` にも対応しています。
また、一部の MOD と多くのアニメーション MOD は正常に変換できない場合があります。
:::

## オプション

ツール画面では、次のオプションを設定できます。

- **Asset Layout Path**:
  3DMigoto のアセットパスを指定します。このオプションは変更時に永続保存されます。詳しくは [アセット](#アセット) を参照してください。

- **Target Mod Path**:
  変換対象となる MOD のパスを指定します。

- **Output GLB Path**:
  変換された GLB モデルファイルの保存先を選択します。

- **Texture Format**:
  テクスチャ画像の形式を選択します。  
  変換速度と GLB サイズの削減のため、`JPEG (Alpha Safe)` モードの使用を推奨します。

## 変換

変換を実行すると、出力先に次のようなフォルダーとファイルが作成されます。

```text
output/
├─ glb/
│  └─ 変換済み GLB ファイル
├─ ui/
│  └─ トグルビューア用アセットファイル
└─ manifest.json
```

変換された GLB ファイルは `glb` フォルダー内で確認できます。

## アセット

`原神`、`崩壊：スターレイル`、`ゼンレスゾーンゼロ` では、GLB 生成のためにアセットが必要です。各ゲームの公式アセットリポジトリは次のとおりです。

- 原神: [SilentNightSound/GI-Model-Importer-Assets](https://github.com/SilentNightSound/GI-Model-Importer-Assets)
- 崩壊：スターレイル: [SilentNightSound/SR-Model-Importer-Assets](https://github.com/SilentNightSound/SR-Model-Importer-Assets)
- ゼンレスゾーンゼロ: [leotorrez/ZZ-Model-Importer-Assets](https://github.com/leotorrez/ZZ-Model-Importer-Assets)
- 鳴潮: [SpectrumQT/WWMI-Assets](https://github.com/SpectrumQT/WWMI-Assets)

必要なゲームのアセットをダウンロードした後、次のように配置します。

```text
assets/
├─ GI-Model-Importer-Assets/
├─ SR-Model-Importer-Assets/
├─ ZZ-Model-Importer-Assets/
└─ WWMI-Assets/
```

その後、`assets` フォルダーを **Asset Layout Path** として指定します。

::: tip
各リポジトリが常にゲームの最新状態を反映しているとは限りません。特定のキャラクターや武器のアセットが不足していたり、データが古い場合は、[gui_collect](https://github.com/Petrascyll/gui_collect) などのツールを使って自分でアセットをダンプできます。
:::

::: tip
アセットファイルのうち、GLB 変換に必要なのは `vb`、`ib`、`fmt`、`json`、`txt` ファイルだけです。GLB 変換専用に使う場合は、容量の大きい `dds` ファイルを削除しても問題ありません。
:::
