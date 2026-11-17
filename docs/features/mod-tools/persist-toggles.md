# Persist Toggles

3DMigoto stores the values of toggle variables marked `persist` in `d3dx_user.ini`, not in the mod's INI file. If you run Reload (`F10`) while the mod is disabled, the value can be removed, and the mod's INI default applies when the mod is enabled again.

## How It Works

When this feature is on, Nahida Desktop watches each importer's `d3dx_user.ini` and writes the saved toggle values into the corresponding mod INI files. Toggle states then remain after the mod is disabled or reloaded.

## Copied Mods and Namespaces

The INI to write is found by the namespace of the INI that declares each variable, including an explicit `namespace = ...`. If several INIs declare the same variable in the same namespace, that variable is skipped and the conflicting paths are recorded in the log. Disabled mods are not part of this check.

Renaming a folder does not change an explicit namespace. To keep copied mods' toggle states independent, give each copy a distinct namespace and update the references in its help/menu INIs. Nahida Desktop does not change namespaces or references.

## Turn It On

Open `Persist Toggles` on the Tools page and turn on the switch. The log below the switch lists saved values and skipped variables.

::: warning
This feature requires XXMI to be set up first. See [Set Up XXMI](/others/set-up-xxmi).
:::
