# 切换状态持久化

3DMigoto 会把标记为 `persist` 的切换变量状态保存到 `d3dx_user.ini` 中，因此即使重新启动游戏，也能保留之前的切换状态。

但这些值只会保存在 `d3dx_user.ini` 中，而不会写回模组自己的 INI 文件。因此，如果在禁用模组的状态下执行 Reload (`F10`)，对应的切换值可能会被移除。之后重新启用模组时，可能就会应用模组 INI 文件中的默认值，而不是之前的状态。

## 功能说明

启用该功能后，Nahida Desktop 会监听各个 Importer 的 `d3dx_user.ini` 变化。

当你在游戏内修改切换项，并因 Reload (`F10`) 导致 `d3dx_user.ini` 更新时，Nahida Desktop 会自动把 `d3dx_user.ini` 中保存的切换值写回对应模组的 INI 文件。

这样一来，即使禁用模组或重新加载模组，也可以继续保留之前的切换状态。

## 使用前确认事项

::: warning
要使用此功能，必须先配置 XXMI 路径。  
详情请参考 [连接 XXMI](/zh-CN/others/set-up-xxmi)。
:::
