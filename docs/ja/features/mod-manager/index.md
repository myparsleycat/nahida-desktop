# MOD マネージャー

Nahida Desktop には MOD 管理機能が組み込まれています。MOD マネージャーでは、MOD の有効状態を管理できるだけでなく、MOD の追加、プレビューの設定、グループ管理など、さまざまな機能を利用できます。

![画像](/features/mod-manager/0.png)

MOD マネージャーページは、大きく左側のサイドバーと右側の MOD リスト領域に分かれています。

## ゲームを追加

![画像](/features/mod-manager/add-game.png)

サイドバー下部の `+` アイコンを押すと、ゲーム追加ダイアログを開けます。

- `ゲーム名`: MOD マネージャー内のゲーム一覧に表示される名前を入力します。
- `MOD フォルダーのパス`: 右側のフォルダーアイコンを押して、そのゲームの MOD フォルダーを選択します。
  - `C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods`
  - `D:\Mods\Genshin`
  - など
- `Importer`: [XXMI パス](/ja/others/set-up-xxmi) が設定済みの場合、そのゲームで使う XXMI Importer を選択できます。

## フォルダー構成

推奨される MOD フォルダー構成は次のとおりです。

```txt
Character
├─ Aino
│  ├─ AinoMod1
│  └─ AinoMod2
└─ Amber
   ├─ AmberMod1
   └─ AmberMod2
Enemy
└─ EnemyMod1
NPC
└─ Katheryne
   └─ KatheryneMod1
```

::: info
設定した MOD フォルダーの直下にある 1 階層目のフォルダーがサイドバーに表示されます。

そのため、設定した MOD フォルダー内に直接 MOD を置くのではなく、`Character`、`Enemy`、`NPC` のような分類フォルダーを先に作成し、その中に MOD フォルダーを入れることを推奨します。

たとえば、MOD フォルダーを次のパスに設定した場合:

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods
```

次の構成は推奨されません。

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods\SomeCharacterMod
```

代わりに、少なくとも 1 段階以上の分類フォルダーを挟んだ次のような構成を推奨します。

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods\Character\SomeCharacterMod
```

:::

### サブグループ

![画像](/features/mod-manager/1.gif)

`Character` フォルダーのように、内部に 1 つ以上のサブフォルダーを持つグループは、サイドバーで右クリックしてサブグループに指定できます。

サブグループに指定されたグループは、クリックして折りたたみや展開ができます。

## MOD を追加

サイドバーでグループを選択した状態で、次の方法から新しい MOD を追加できます。

- GameBanana から MOD をダウンロードします。
- MOD フォルダーまたは圧縮された MOD ファイルを MOD リストにドラッグ＆ドロップします。
- MOD リスト上部ヘッダーの `...` ボタンをクリックし、`ダウンロード` を選択して、ダウンロードするファイルの URL を入力します。

## プレビュー

Nahida Desktop の MOD マネージャーは、MOD フォルダー内でファイル名が `preview` の画像または動画ファイルを大文字小文字を区別せずに探し、プレビューとして表示します。

プレビューに使用できるメディア形式は `jpeg`、`png`、`webp`、`avif`、`mp4`、`webm` です。

`preview` という名前のメディアファイルが存在しない場合は、フォルダー内で昇順に最初に見つかった対応メディアをプレビューとして使用します。使用可能なメディアがなければ、プレビューは表示されません。

::: info
設定 → MOD タブの `サブフォルダーからプレビューを検索` オプションが有効な場合、サブフォルダーも再帰的に検索してプレビューを探します。
:::

### プレビューを設定または差し替え

サイドバーのグループには、画像または動画ファイルをドラッグ＆ドロップしてプレビューを設定できます。

MOD の場合は、プレビューの有無で手順が変わります。

- プレビューがない場合: メディアファイルをコピーしてから、プレビューセクションの貼り付けボタンをクリックします。
- プレビューがある場合: 既存のプレビューを右クリックして、別のメディアファイルに差し替えます。

## ゲームのクイック起動

![画像](/features/mod-manager/quick-start.gif)

[XXMI パス](/ja/others/set-up-xxmi) が設定されていれば、XXMI Launcher を開かずに MOD マネージャーからゲームを起動できます。

1. ゲーム一覧で右側の鉛筆アイコンをクリックして設定ダイアログを開きます。
2. そのゲームで使う Importer を選択し、保存ボタンをクリックします。
3. ゲーム項目の左側にある再生アイコンをクリックしてゲームを起動します。
