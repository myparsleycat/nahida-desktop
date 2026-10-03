# Persist Toggles

3DMigoto stores the state of toggle variables marked with `persist` in `d3dx_user.ini`, so their previous states remain even after the game is launched again.

However, those values are stored only in `d3dx_user.ini`, not in the mod's INI file. Because of that, if you run Reload (`F10`) while the mod is disabled, the toggle value can be removed. When the mod is enabled again later, the default value from the mod INI file may be applied instead of the previous state.

## How It Works

When this feature is enabled, Nahida Desktop watches for changes to each importer's `d3dx_user.ini`.

After you change a toggle in-game and `d3dx_user.ini` is updated by Reload (`F10`), Nahida Desktop automatically writes the saved toggle value from `d3dx_user.ini` back into the corresponding mod INI file.

As a result, the previous toggle state can remain intact even after disabling or reloading the mod.

## Copied Mods and Namespace Isolation

Automatic namespace isolation has its own switch in the **Namespace collisions** section. It is off by default and only runs while **Persist Toggles** is also on. While it is on, Nahida Desktop scans the INIs in your mod folders at startup, when the folders change, right before a game launch, and periodically, which adds disk activity. While it is off, none of those scans run; collisions are checked and reported only when you press **Rescan**.

For safe duplicate explicit namespaces across different mods, Nahida Desktop assigns independent namespaces to **all participants**, including disabled copies, and updates the related INI references. Copies made outside Nahida Desktop are detected too, including after restarting the app.

Isolation only applies to explicit `namespace = ...` declarations. INIs within the same mod may share a namespace, for example a main INI and help/menu INIs; that alone is not a collision. NTE is excluded. Display names in the GUI stay unchanged: namespace suffixes are internal and are not added to mod names.

When the game is running, changes are deferred until it exits. The collision section shows **Waiting for game exit**, **Needs review**, **Failed**, or **Recovery required**, together with the reason, details, and affected mod/INI paths. Use the importer's **Rescan** button to request another check. There is no force action, game-stop action, or requirement to press F10 for isolation.

Namespace mappings are recorded in the mod's `nhd` metadata. Backups and recovery information protect the INI changes. If recovery is required, review the reported paths and details before changing files. Nahida Desktop does not transfer old `d3dx_user.ini` values when their ownership is uncertain; ambiguous persistent variables are skipped and logged rather than assigned to a copy. Other unambiguous variables continue to save.

Even with automatic isolation off, launching a game checks mod folder boundaries and transaction journals without reading the INIs. Unfinished or unreadable journals block launch until resolved. Use **Rescan** to recover an interrupted transaction; recovery remains available while automatic isolation is off.

## Before You Use It

::: warning
To use this feature, you must configure the XXMI path first.  
For details, see [Set Up XXMI](/others/set-up-xxmi).
:::
