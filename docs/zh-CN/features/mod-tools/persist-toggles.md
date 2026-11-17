# 持久保存切换状态

3DMigoto 会把标记为 `persist` 的切换变量的值保存到 `d3dx_user.ini`，而不是模组自身的 INI 文件中。因此，如果在禁用模组的状态下执行 Reload (`F10`)，该值可能会被移除；重新启用模组后，会应用模组 INI 文件中的默认值。

## 功能说明

启用该功能后，Nahida Desktop 会监视各 Importer 的 `d3dx_user.ini` 的变化，并将其中保存的切换值写入对应模组的 INI 文件。禁用或重新加载模组后，切换状态仍会保留。

## 复制的模组与 namespace

写入目标 INI 根据声明各变量的 INI 所属的 namespace（包括通过 `namespace = ...` 显式指定的名称）查找。如果多个 INI 在同一个 namespace 中声明了同一个变量，则不保存该变量，并在日志中记录冲突文件的路径。已禁用的模组不参与此检查。

修改文件夹名称不会改变显式指定的 namespace。要让复制的模组各自保留切换状态，需要为每个副本设置不同的 namespace，并修改其 help、菜单 INI 中的相关引用。Nahida Desktop 不会修改 namespace 或引用。

## 开启功能

在 Tools 页面中打开 `持久保存切换状态`，然后打开开关。开关下方的日志会列出已保存的值和被跳过的变量。

::: warning
使用此功能前，需要先完成 XXMI 设置。详情请参阅[连接 XXMI](/zh-CN/others/set-up-xxmi)。
:::
