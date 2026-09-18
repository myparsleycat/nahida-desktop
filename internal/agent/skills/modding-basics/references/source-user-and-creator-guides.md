# Imported user and creator guides

> Operational boundary: this is local reference material, not authorization to browse websites, download or install tools, execute scripts or binaries, or start a launcher or game. Blender may be inspected and operated only through a capable enabled Blender MCP and within the user's requested scope; if it is unavailable, explain the Blender steps manually. External links and all other named tools are informational only. Verify claims from available local files or registered tools.
>
> Version boundary: examples, hashes, key bindings, tool interfaces, and game-specific fixes may be stale or importer-specific. Treat them as diagnostic context rather than current facts.
>
> Provenance: adapted from the locally supplied leotorrez/modding repository at revision 344a7cbfc1a03693205dfa8c01da312a68b8c841 under GPL-3.0. Image-only lines and VitePress presentation markup were removed; substantive text and code examples were retained.

## Imported source: `guides/index.md`

# Guides

The following guides are set up in roughly the order they should be read in.

## Glosary

- **Mod user**: Person who uses mods in XXMI games.
- **Mod maker**: Person who creates mods for XXMI games.
- **Fix**: Term often used to refer to scripts or programs that fix a specific issue in mods for a given version.

## Mod usage tutorials

- [Getting started with XXMI mods](./getting-started.md)
- [FAQ](./faq.md)
- [Getting Mods](./getting-mods.md)
- [Launchers](./launchers.md)
- [Mod Managers](./mod-managers.md)
- [Troubleshooting](./troubleshooting.md)

## Mod making tutorials

- [Modding 101](./modding-101.md)
- [Textures 101](./textures-101.md)
- [XXMI Tools](./xxmi_tools.md)
- [Blender tips](./blender-tips.md)
- [Weapon banana](./weapon-banana.md)
- [Mona Hat tutorial](./mona-hat.md)
- [Hunting & Dumping](./hunting.md)
- [Shaders 101](./shaders-101.md)
- [ZZZ Textures](./zzz-textures.md)

## Imported source: `guides/getting-started.md`

# Getting Started

## Quick Start Guide

1. Download and install the latest version of the [Launcher](https://github.com/SpectrumQT/XXMI-Launcher/releases/latest)

2. Open the launcher, select your game of choice, and click "Start"

- You will be presented with this screen that teaches you the basic keybinds to use 3dmigoto. Read it carefully and press the key mentioned to close it.

3. **Done!** You're now playing with mods active (Sucrose cursor not included)

---
You might be wondering how it can be this simple. As a matter of fact, it is! The launcher will automatically download and install the necessary configurations for you, so you can focus on playing the game and acquiring the mods of your preference.

## Installing your first mod

1. Head over to your modding site of choice. We recommend [gamebanana.com](https://gamebanana.com) but sites like [nexusmods.com](https://nexusmods.com) or [loverslab.com](https://loverslab.com) are also good sources.
    - [GI mods](https://gamebanana.com/games/8552)
    - [WW mods](https://gamebanana.com/games/20357)
    - [HSR mods](https://gamebanana.com/games/18366)
    - [ZZZ mods](https://gamebanana.com/games/19567)
2. Browse the site and download the mod you want to install. They tend to be in a `.zip` file (or similar) with all the necessary files inside.
3. Open the launcher, click on the "Open Mods folder"

4. [Extract the zip file](https://www.google.com/search?q=how+to+extract+a+compressed+file) you downloaded into the mods folder
5. Head over to your game and press F10 to hot reload the mods.
6. Done! You're now playing with your first mod active.

It is worth mentioning that some mods might require you to download other mods to function or even install them in specific folders. Make sure to read the mod's description to know if that's the case.

Be mindful of your security when downloading mods. Always make sure to download from trusted sources. Mods are only composed of `.ini` files, textures, and 3D models of `.buf` extension. If any mod asks you to download an executable file (`.exe`) or `.dll`, it's likely a virus. There are exceptions to this rule, such as tools or fixes that require `.py` or `.exe` files to run. Read more about it [here](./troubleshooting.md#fixing-mods)

## Migrating from GIMI/HIMI/SRMI/ZZMI

If you are coming over from the old modding tools, you simply need to install the launcher as the steps above explain and move the content of your `/Mods` folder to the new mods folder. You can find out where it is by clicking on `Open Mods Folder` in the launcher.

Once done, you can simply press F10 in-game to reload your mods and you're good to go!

## Imported source: `guides/modding-101.md`

# Requirements

In order to start making mods you will need the following:

- [Blender latest LTS(Long Term Support) version](https://blender.org/downloads). 3D modeling software
- An [XXMI Launcher](/guides/getting-started.md) Install with developer mode enabled. This is required to test your mods in the game
- [XXMI Tools](/guides/xxmi_tools.md#installation) plugin for Blender. Plugin to export models for reinjection

## Getting Started with Blender

Blender is quite a powerful and complete tool for 3D modeling, animations, rendering and even video edition. For this reason, it can be a bit overwhelming at first. Before diving into it and start pressing random buttons, it is recommended to go through some basic tutorials to get familiar with the interface and some keybinds.

The channel [Blender Guru](https://www.youtube.com/@blenderguru) on YouTube has an excellent series called "Donut Tutorial." It's a great starting point for beginners, covering Blender basics in a fun and engaging way.

## Next Steps: Where to Start

Here is a non-exhaustive list of easy tutorials to get started that can introduce you to modding concepts

- [Mona Hat](/guides/mona-hat.md) - How to remove parts of a model
- [Weapon Banana](/guides/weapon-banana.md) - A basic tutorial on weapon modding
- How to make GI Mods:

- How to make ZZZ Mods:

After familiarizing yourself with the basics of modding, explore other tutorials on this website. If you are more interested in the coding aspects, check out the [INI Docs](/docs/index.md).

---

Most of these tutorials cover the export process and explain or showcase it. However, they do not focus on the export tools.
For more detailed guides on them and their advanced features, you can check the following:

- [XXMI Tools Guide](/guides/xxmi_tools.md)
- [WWMI Tools Guide](/guides/wwmi_tools.md)

## Imported source: `guides/faq.md`

# Frequently Asked Questions

## Can I get banned for using mods(cosmetics)?

The short answer is technically yes, but practically no. The long answer is that the ToS of these games state that anything that modifies the game memory and even third-party software can be considered a bannable offense. These clauses are there to protect the game from cheaters and hackers first and foremost. However, the developers of these games have not taken any action against modders in the past, and it is unlikely they will in the future.

The fact that there are over 300,000 active players using mods and no one has been banned yet is a testament to this. The developers have even acknowledged the modding community in the past, so it is safe to say that you won't be banned for using mods.

### What about the banwave on HSR release?

The banwave was targeting resellers of accounts, rerolls, and cheaters. In the process, their anticheat system misflagged things like Linux devices, FPS unlockers, Reshade, and even some input device software. There have been users who happened to be using mods when this happened, and the paranoia got the best of them. After checking every report in detail, we concluded that in all cases there were other tools or software that could have triggered the ban.

## Do I need to hide my UID?

This is a big misconception. Most people in the modding scene will recommend hiding your UID for security reasons as if to hide from the developers. Realistically, the developers have way easier ways to track if someone is using mods or not. In fact, they know exactly which accounts do and which don't. There are claims that content creators partnered with the developers might be banned via their UID. They would likely be identified by their online handle way faster than their UID, so this is once more a fallacy.

The only valid reason to hide your UID is for aesthetic reasons or to avoid people from sending you coop requests. In which case, you will easily find mods that help you hide your UID. You are one Google search away from finding them!

## Can coop players see my mods?

No. Mods are client-side only, and there is no networking functionality built into them. The only way for someone to see your mods is by sharing screenshots, videos, or streaming.

## A mod requires I install another .dll file, should I?

It is a massive security risk to do so. A .dll file can execute arbitrary code on your device and do as it pleases within. If you are unsure about a mod, ask in the modding community or the modder themselves. If you are still unsure, don't install it.

There are old guides recommending replacing the .dll that comes with the launcher, and some even worse recommend putting it in the game folder. This will likely get you banned because it counts as modifying the game files (by adding content to their directory). Ignore said guides and make sure to always install mods in a separate folder. If a mod requires you to download a random .dll, request the author to amend this, and if you paid for said mod, file a refund with your credit card.

## Will modding be possible outside of PC?

The tools used for modding on PC are not designed to work on any other device. In fact, it is Windows-only software. There are no plans to port the tools to other devices, and even if there were, the process would be long and tedious, probably taking several years of development.

## Imported source: `guides/getting-mods.md`

# Recommended websites for mods

- [GameBanana](https://gamebanana.com)
- [Nexus Mods](https://www.nexusmods.com)
- [Lovers Lab](https://www.loverslab.com)

## Etiquette for modding

- Always read the mod description and requirements before downloading. In some cases mods depend on other works from the community to work, often refered as libraries, requirements or dependencies. Follow the instructions that come with them to properly configure them prior to installing your mod. Otherwise you might endup with a broken looking mod.
- Always endorse/like the mods you like. This is the best way to show your appreciation to the mod author and to incentivize them into making more of that kind.
- Beware of your security. Mods for 3dm games are distributed in compressed files and contain `.dds` images for texturing, `.ini` files for configurations, `.buf` files to store the 3d models into and ocassionally `.hlsl` for custom shaders. Any other file type should be treated with caution, because it might be some form of malware. The prior mentioned files can't be infected with a virus due to their primitive nature.

## Paid content and piracy

In the communities mentioned across this website paywalling is forbidden. In order to monetize content, mod authors rely on donations, patreon and other forms of crowdfunding. The use of early access content is the more acceptable form of monetization, as it allows the mod author to get some revenue from their work, while the community can still enjoy the mod openly and for free after a certain period of time(usually up to 30 days from initial release).

The use of websites to circumvent these early access periods is seen as a form of piracy and frown upon on the community. It is important to respect the work of the mod authors and the rules they set for their content after all is the product of their hard labor.

## Closed communites

There are a fair share of websites, forums and closed Discord servers or QQ groups that distrubute mods in closed circles with their own set of rules and customs. Sadly these tend to be quite hard to get into and closed to foreign audiences. At times they lock their content behind paywalls or passwords that can have cultural connotation or subtleties to keep the community tight knit. It is important to respect these communities and their rules, as they help keep the modding scene alive in their own way. Requesting others to share content from these communities is seen as disrespectful and can lead to a ban from their community and a bad reputation in the modding scene.

Beware that some of these are scams and seek to charge an entry fee or a subscription to access their content. Always be cautious and do your research before joining any of these communities. In general we advice against paying for mods that will never be public because they tend to be parts of these scams.
At times these communities are closed because the mods they distribute are stolen from other authors and reposted behind a paywall. Not only you can potentially find them for free from their original authors but also you are supporting a thief by paying for their content. When in doubt simply reach out to others in the [discord server](https://discord.gg/agmg) for advice.

## Imported source: `guides/launchers.md`

# Launchers

Launchers can help you set up your 3dmigoto configuration faster and without much hassle. You might have seen me recommend XXMI Launcher in the quick started guide already. That launcher is developed by SpectrumQT whom works in the AGMG community alongside me and other developers. It reduces the complexity of getting started with mods to mere plug and play while also serving as an auto updating tool. As the games update, some mods break and fixes are required, prior to the use of this launcher said fixes had to be manually excecuted by the user and installed in the proper places which lead to a lot of user side mistakes and confusion.

## Other Launchers

Apart from XXMI Launcher, there are other communities creating their own. Personally I can't recommend any and some I even actively advice against due to the fact they monetize something that is free and open source without adding any value on top of what is already readily available elsewhere. Exercise caution when downloading and using other launchers. As a rule of thumb avoid paid options and ones that don't openly share their source code, as they might be injecting code on the back without you knowing.

## XXMI Launcher

I will elaborate on the utility and functionality of this one in particular however other launchers will more likely have all the same functionality built-in in one form or another.

### One-click updates

The launcher will automatically check for updates and download them for you. This is a great feature as it ensures you are always up to date with the latest fixes and improvements. If you disable auto updating, they will show on the main screen and you can manually update them at your own pace.

### Reshade - FPS unlocker support

FPS unlocker comes built-in with the launcher for the games that can make use of it. Activating it is as simple as selecting your target FPS in the settings. Upon game relaunch, the FPS unlocker will be active and you will be able to enjoy your game at the desired frame rate.

As for reshade it does not come built-in but setting it up tends to be quite simple.
Here is a guide by caverabbit on the subject: <https://gamebanana.com/tools/18082>

### Adding functionality to the launcher

In the ADVANCED tab within your settings, you can set up pre and post scripts/programs to run before and after the game starts. This can be used to launch any other program that you might be interest to use alongside mods. Reshade is an example of this. Personally I've written a python script that randomize which outfit my character will use for the gaming session. The possibilities are endless. Feel free to experiment and brainstorm ideas with the community. Even if you consider yourself uncapable of making it, someone else might be able to help you out. Modding doesn't have to be limited to the 3D models only. Whatever that adds flavour to the experience is welcomed.

## Imported source: `guides/mod-managers.md`

# Mod Managers

Due to technical complexity making a mod manager that can account for all possible ways a mod maker would craft their creation is near impossible. Hence mod managers need to be very sophisticated to even somewhat function. Games like Skyrim or Fallout don't have this issue due to the file organization required for their mods to function making them easy to manage. 3DMigoto makes it hard to identify resources and group them logically. Hence there is a low abundance in mod managers and most are (at the time of writing this guide) in quite a primitive state.

Therefore, I recommend to make a backup of your mod folder before even testing a mod manager. Most of them mess with your file organization and some even edit your configuration files to the point that they can accidentally break the mod outside of the envoirament they created. 

## I am no coward. Give me those links

Alright alright, disclaimers aside mod managers are quite interesting projects and they need more support and aid to polish their work. I will be linking the ones I am aware of without much explanation of their functionality both because they explain it in their own links and because their functionalities are always evolving. 

- In-game mod manager by [Nurarihyon]
    - [In-Game 'Mod Manager' for GIMI](https://gamebanana.com/tools/17415)
    - [In-Game 'Mod Manager' for ZZMI](https://gamebanana.com/tools/17807)
    - [In-Game 'Mod Manager' for WWMI](https://gamebanana.com/tools/17435)
- [JASM - Just Another Skin Manager](https://gamebanana.com/tools/14574)
- [GI Mod Manager](https://gamebanana.com/tools/12471)
