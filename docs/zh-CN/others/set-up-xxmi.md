# 连接 XXMI

本页介绍如何设置 XXMI Launcher 路径，以及设置完成后可以使用的附加功能。

## 配置路径

![图片](/others/set-up-xxmi/xxmi-page.png)

1. 点击左侧边栏中的设置图标，进入设置页面。
2. 在设置页面的分区侧边栏中选择 `XXMI`。
3. 在 `设置 XXMI 路径` 输入框中填写 XXMI Launcher 路径，然后点击保存按钮。

如果输入的是有效路径，并且该 XXMI Launcher 中已有正在使用的 Importer，则会直接显示已启用的 Importer 列表，不会额外弹出错误提示。

如果你没有修改安装位置，XXMI Launcher 默认通常安装在以下路径：

```txt
%AppData%\XXMI Launcher
```

如果默认路径中找不到 XXMI Launcher，或者你不记得安装位置，也可以使用 `自动检测` 功能。

::: warning
自动检测功能会扫描所有已激活的磁盘驱动器来查找 XXMI Launcher。

如果系统中存在多个 XXMI Launcher 安装目录，自动检测可能不会选中你期望的那个路径，因此建议你再次确认检测结果。
:::

## 配置路径后可用的功能

完成 XXMI 路径配置后，可以使用以下功能：

- 快速启动游戏
- d3d11.dll 构建器
- 切换状态持久化
- 切换查看器生成器
