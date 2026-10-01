# Persist Toggles

3DMigoto stores the state of toggle variables marked with `persist` in `d3dx_user.ini`, so their previous states remain even after the game is launched again.

However, those values are stored only in `d3dx_user.ini`, not in the mod's INI file. Because of that, if you run Reload (`F10`) while the mod is disabled, the toggle value can be removed. When the mod is enabled again later, the default value from the mod INI file may be applied instead of the previous state.

## How It Works

When this feature is enabled, Nahida Desktop watches for changes to each importer's `d3dx_user.ini`.

After you change a toggle in-game and `d3dx_user.ini` is updated by Reload (`F10`), Nahida Desktop automatically writes the saved toggle value from `d3dx_user.ini` back into the corresponding mod INI file.

As a result, the previous toggle state can remain intact even after disabling or reloading the mod.

## Copied Mods and Namespaces

Nahida Desktop finds the INI that declares each persistent variable using its effective namespace, including an explicit `namespace = ...`. Main and help/menu INIs can share a namespace when they declare different variables.

If multiple INIs declare the same variable in the same namespace, Nahida Desktop skips that variable and records the conflicting paths in the persist log. Disabled copies are included in this check because a value saved during a mod switch may still belong to the previously loaded copy. Other unambiguous variables continue to save.

Changing a folder name does not change an explicit namespace. To keep copied mods independent in-game, give each copy a distinct namespace and update the corresponding references in its help/menu INIs. Nahida Desktop does not automatically rename these namespaces or rewrite their references.

## Before You Use It

::: warning
To use this feature, you must configure the XXMI path first.  
For details, see [Set Up XXMI](/others/set-up-xxmi).
:::
