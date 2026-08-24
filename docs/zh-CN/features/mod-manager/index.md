# 模组管理器

Nahida Desktop 内置模组管理功能。在模组管理器中，你不仅可以管理模组的启用状态，还可以使用添加模组、设置预览、管理分组等功能。

![图片](/features/mod-manager/0.png)

模组管理器页面主要分为左侧边栏和右侧模组列表区域。

## 添加游戏

![图片](/features/mod-manager/add-game.png)

点击边栏底部的 `+` 图标即可打开添加游戏对话框。

- `游戏名称`：输入显示在模组管理器游戏列表中的名称。
- `模组文件夹路径`：点击右侧文件夹图标，选择该游戏的模组文件夹。
  - `C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods`
  - `D:\Mods\Genshin`
  - 等
- `Importer`：如果已配置 [XXMI 路径](/zh-CN/others/set-up-xxmi)，则可以选择该游戏要使用的 XXMI Importer。

## 文件夹结构

推荐的模组文件夹结构如下：

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
已配置模组文件夹下的第一层子文件夹会显示在侧边栏中。

因此，不建议直接把模组放在配置的模组文件夹根目录下。更推荐先创建 `Character`、`Enemy`、`NPC` 这类分类文件夹，再把模组文件夹放进去。

例如，如果你的模组文件夹配置为：

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods
```

则不推荐使用下面这种结构：

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods\SomeCharacterMod
```

更推荐至少增加一层分类目录，例如：

```txt
C:\Users\User\AppData\Roaming\XXMI Launcher\GIMI\Mods\Character\SomeCharacterMod
```

:::

### 子分组

![图片](/features/mod-manager/1.gif)

像 `Character` 这样内部包含一个或多个子文件夹的分组，可以在侧边栏中右键并标记为子分组。

被标记为子分组的项目可以通过点击来折叠或展开。

按住 `Ctrl` 点击分组会将其子文件夹展开到树中（已展开时保持展开）并选中该分组。按住 `Alt` 点击子分组可以折叠/展开其父分组。

## 添加模组

在侧边栏中选中某个分组后，可以通过以下方式添加新模组：

- 从 GameBanana 下载模组。
- 将模组文件夹或压缩包直接拖放到模组列表中。
- 点击模组列表顶部标题栏中的 `...` 按钮，选择 `下载`，然后输入要下载文件的 URL。

## 预览

Nahida Desktop 的模组管理器会在模组文件夹中查找文件名为 `preview` 的图片或视频文件，查找时不区分大小写，并将其作为预览显示。

可用作预览的媒体格式包括 `jpeg`、`png`、`webp`、`avif`、`mp4`、`webm`。

如果没有名为 `preview` 的媒体文件，则会按升序使用文件夹中找到的第一个可用媒体文件作为预览。如果没有任何可用媒体文件，则不会显示预览。

::: info
如果在 设置 → 模组 中启用了 `在子文件夹中搜索预览`，应用也会递归搜索子文件夹中的预览文件。
:::

### 设置或替换预览

对于侧边栏中的分组，你可以通过拖放图片或视频文件来设置预览。

对于模组，操作方式会根据当前是否已有预览而不同：

- 没有预览时：先复制媒体文件，再点击预览区域中的粘贴按钮。
- 已有预览时：右键点击现有预览，并替换为其他媒体文件。

## 快速启动游戏

![图片](/features/mod-manager/quick-start.gif)

如果已经配置了 [XXMI 路径](/zh-CN/others/set-up-xxmi)，就可以直接从模组管理器启动游戏，而无需先打开 XXMI Launcher。

1. 在游戏列表中点击右侧的铅笔图标，打开设置对话框。
2. 选择该游戏要使用的 Importer，然后点击保存按钮。
3. 点击游戏项左侧的播放图标来启动游戏。
