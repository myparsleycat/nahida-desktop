# Character Data

The Model Viewer uses the game data of the character set for a mod to provide these features.

- **Character motion**: Plays the character's game motions on the mod.
- **Original game data**: Fills in the face, hair, and textures the mod does not include with the game's originals.
- **Expressions**: Applies expressions and blinking to the original face.
- **Per-character toon style**: Uses the character's material values for toon shading.

Supported games are Genshin Impact (GIMI), Honkai: Star Rail (SRMI), Zenless Zone Zero (ZZMI), Wuthering Waves (WWMI), and Arknights: Endfield (EFMI). These features are available only for characters whose data is provided.

## Set a Character

To use character data, set a character on the sidebar folder that contains the mod.

1. In the Mod Manager sidebar, right-click a folder and select `Set Character…`.
2. Select the `Game` and `Character`, then click `Save`.

The Model Viewer uses the character set on the nearest folder above the mod. In [Blender Live Preview](/features/mod-manager/blender-live-preview), select the character in the connection list.

## Downloads

Character data is downloaded from the nahida.live server the first time it is used, and the saved files are used after that. While it downloads, the progress is shown on the [playback controller](/features/mod-manager/model-viewer#playback).

## Character Motion {#character-motion}

Turn on `Character Motion` in the `Model` menu to play the character's game motions on the mod. It is shown when the character has motion data and the mod's meshes have bone weights, and it is on by default.

- Motions have their own lane on the playback controller and start with an idle clip. Selecting another clip in the clip menu blends from the current pose.
- If the mod has its own animations, they play together with the motion.
- `Turn off character motion` on the motion lane turns motion off in the current viewer only.

## Original Game Data {#original-data}

Turn on `Original Game Data` in the `Model` menu to show the game's original data with the mod. It is shown when the character has original data, and it is on by default.

- Face and head parts the mod leaves to the game are drawn from the original model. Face textures in the mod are applied to the original face.
- Empty texture slots of the mod's meshes are filled with the original textures.
- The `Missing Parts` submenu shows or hides original parts the mod does not include. Hair parts the game keeps drawing are shown from the start.

## Expressions {#expression}

When original game data is on and a shown part has expression data, the `Model` menu shows the `Expression` submenu.

- Select `Default`, `Smile`, `Angry`, `Troubled`, `Surprised`, `Wink`, or `Eyes Closed`. Only expressions that apply to the shown parts are listed.
- `Blinking`: Turns automatic blinking on or off.

If a character motion clip contains expressions, the clip's expressions play. Selecting an expression other than `Default` overrides them.

## Per-Character Toon Style {#toon-style}

For mods with a character set, the [toon style](/features/mod-manager/model-viewer#toon-style) submenu shows `Style for {character name}` at the top and uses the character's material values for toon shading.

- In the `Shading` submenu, values taken from the character's material are marked `Character material`.
- In Genshin Impact, `Auto ({character name})` in `Shadow Ramp` uses the character's ramp.
- Toon style changes are saved per character. `Light Follows Camera` and the Genshin texture folder are saved per game.
- `Reset Character Style` resets the values saved for the character.
