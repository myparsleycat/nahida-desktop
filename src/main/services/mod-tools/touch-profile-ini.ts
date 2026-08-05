import fse from "fs-extra";

import type {
    TouchComponentAnalysis,
    TouchComponentDraft,
    TouchModAnalysis,
} from "./touch-profile-types";

import { bakeSampleOffsets, type TouchGeneratedAssets } from "./touch-profile-assets";
import { resolveTouchJiggleParams } from "./touch-profile-settings";
import { DEFAULT_TOUCH_JIGGLE_PARAMS, TOUCH_ZONE_CHANNELS } from "./touch-profile-types";

export type TouchIniCompileInput = {
    sourceIniPath: string;
    targetIniPath: string;
    analysis: TouchModAnalysis;
    drafts: TouchComponentDraft[];
    assets: TouchGeneratedAssets[];
    namespaceToken: string;
    varPrefix: string;
};

type InteractiveEntry = {
    draft: TouchComponentDraft;
    component: TouchComponentAnalysis;
    asset: TouchGeneratedAssets;
};

export async function compileTouchIni(input: TouchIniCompileInput) {
    const original = await fse.readFile(input.sourceIniPath, "utf8");
    const backupPath = `${input.targetIniPath}.bak-before-touch`;
    await fse.writeFile(backupPath, original, "utf8");

    const interactive = input.drafts
        .map((draft) => {
            const component = input.analysis.components.find(
                (entry) => entry.id === draft.componentId,
            );
            const asset = input.assets.find((entry) => entry.componentId === draft.componentId);
            if (!component || !asset || !draft.interactive || draft.zones.length === 0) return null;
            return { draft, component, asset };
        })
        .filter((entry): entry is InteractiveEntry => !!entry);

    if (interactive.length === 0) {
        throw new Error("No interactive components available for INI compile");
    }

    let text = original.replace(/\r\n/g, "\n");
    text = ensureConstants(
        text,
        input.varPrefix,
        interactive.map((entry) => entry.component),
    );
    text = ensureKeys(text, input.varPrefix);
    text = ensurePresentHook(text, input.varPrefix);
    text = patchBlendSections(
        text,
        input.varPrefix,
        interactive.map((entry) => entry.component),
    );
    text = patchIbSections(
        text,
        input.varPrefix,
        interactive.map((entry) => entry.component),
    );
    text = appendRuntime(text, input.varPrefix, interactive);

    await fse.writeFile(input.targetIniPath, text.replace(/\n/g, "\r\n"), "utf8");
    return { backupPath, interactiveCount: interactive.length };
}

function ensureConstants(text: string, varPrefix: string, components: TouchComponentAnalysis[]) {
    if (text.includes(`global $${varPrefix}_active`)) return text;

    const block = [
        "",
        `; Nahida Touch Profile state (${varPrefix})`,
        `global $${varPrefix}_active = 0`,
        `global $${varPrefix}_initialized = 0`,
        `global $${varPrefix}_detect_allowed = 0`,
        `global $${varPrefix}_mode = 0`,
        `global $${varPrefix}_lmb_down = 0`,
        `global $${varPrefix}_rmb_down = 0`,
        `global $${varPrefix}_x_down = 0`,
        `global $${varPrefix}_modifier_down = 0`,
        `global $${varPrefix}_lmb_prev = 0`,
        `global $${varPrefix}_rmb_prev = 0`,
        `global $${varPrefix}_combo_active = 0`,
        `global $${varPrefix}_mouse_down = 0`,
        `global $${varPrefix}_poke_sign = 0`,
        `global $${varPrefix}_poke_mult = 1.0`,
        `global $${varPrefix}_charge_sign = 0`,
        `global $${varPrefix}_lmb_press_time = 0`,
        `global $${varPrefix}_rmb_press_time = 0`,
        `global $${varPrefix}_cursor_x = 0`,
        `global $${varPrefix}_cursor_y = 0`,
        `global $${varPrefix}_screen_w = 1`,
        `global $${varPrefix}_screen_h = 1`,
        `global $${varPrefix}_delta_time = 0.0166667`,
        `global $${varPrefix}_prev_time = 0`,
        ...components.map(
            (component) => `global $${varPrefix}_last_dispatch_${token(component.id)} = -1`,
        ),
        "",
    ].join("\n");

    if (/\[Constants\][^\n]*\n/i.test(text)) {
        return text.replace(/(\[Constants\][^\n]*\n)/i, `$1${block}`);
    }
    return `[Constants]\n${block}\n${text}`;
}

function ensureKeys(text: string, varPrefix: string) {
    const st = sectionToken(varPrefix);
    if (text.includes(`[Key${st}LMB]`)) return text;

    const block = `
[Key${st}LMB]
key = VK_LBUTTON
type = hold
$${varPrefix}_lmb_down = 1
post $${varPrefix}_lmb_down = 0

[Key${st}RMB]
key = VK_RBUTTON
type = hold
$${varPrefix}_rmb_down = 1
post $${varPrefix}_rmb_down = 0

[Key${st}X]
key = X
type = hold
$${varPrefix}_x_down = 1
post $${varPrefix}_x_down = 0

[Key${st}Modifier]
key = VK_MENU
type = hold
$${varPrefix}_modifier_down = 1
$${varPrefix}_mode = 1
post $${varPrefix}_modifier_down = 0
post $${varPrefix}_mode = 0
`;

    if (/\[Present\]/i.test(text)) {
        return text.replace(/(\[Present\])/i, `${block}\n$1`);
    }
    return `${text.trimEnd()}\n${block}\n`;
}

function ensurePresentHook(text: string, varPrefix: string) {
    const st = sectionToken(varPrefix);
    const presentMatch = text.match(/\[Present\]([\s\S]*?)(?=\n\[|$)/i);
    if (!presentMatch) {
        return `${text.trimEnd()}\n\n[Present]\nrun = CommandList${st}Present\npost $${varPrefix}_active = 0\n`;
    }

    let body = presentMatch[1];
    if (!body.includes(`CommandList${st}Present`)) {
        body = `\nrun = CommandList${st}Present${body}`;
    }
    if (!body.includes(`post $${varPrefix}_active = 0`)) {
        body += `post $${varPrefix}_active = 0\n`;
    }
    return text.replace(presentMatch[0], `[Present]${body}`);
}

function patchBlendSections(text: string, varPrefix: string, components: TouchComponentAnalysis[]) {
    let next = text;
    for (const component of components) {
        if (!component.blendSectionName) continue;
        const header = `[TextureOverride${component.blendSectionName}]`;
        const section = matchSection(next, header);
        if (!section || section.body.includes(`$${varPrefix}_active = 1`)) continue;

        const patchedBody = section.body.includes("$active = 1")
            ? section.body.replace(/(\$active\s*=\s*1[^\n]*\n)/i, `$1\t$${varPrefix}_active = 1\n`)
            : `${section.body}\t$active = 1\n\t$${varPrefix}_active = 1\n`;
        next = next.replace(section.full, `${header}${patchedBody}`);
    }
    return next;
}

function patchIbSections(text: string, varPrefix: string, components: TouchComponentAnalysis[]) {
    let next = text;
    const st = sectionToken(varPrefix);
    const groups = new Map<string, TouchComponentAnalysis[]>();
    for (const component of components) {
        if (!component.ibSectionName) continue;
        const group = groups.get(component.ibSectionName) ?? [];
        group.push(component);
        groups.set(component.ibSectionName, group);
    }

    for (const [ibSectionName, group] of groups) {
        const header = `[TextureOverride${ibSectionName}]`;
        const section = matchSection(next, header);
        if (
            !section ||
            group.some((component) =>
                section.body.includes(`CustomShader${st}Bake${token(component.id)}`),
            )
        ) {
            continue;
        }

        const inject = group
            .map((component) => buildIbInjection(component, varPrefix, st))
            .join("\n");
        const ibLine = section.body.match(/^\s*ib\s*=\s*.+$/im);
        if (ibLine) {
            next = next.replace(
                section.full,
                `${header}${section.body.replace(ibLine[0], `${ibLine[0]}\n${inject.trimEnd()}`)}`,
            );
            continue;
        }

        const runMatches = [...section.body.matchAll(/^\s*run\s*=\s*CommandList[^\r\n]*$/gim)];
        const lastRun = runMatches.at(-1);
        if (lastRun?.index !== undefined) {
            const insertionPoint = lastRun.index + lastRun[0].length;
            const body = `${section.body.slice(0, insertionPoint)}\n${inject.trimEnd()}${section.body.slice(insertionPoint)}`;
            next = next.replace(section.full, `${header}${body}`);
            continue;
        }

        next = next.replace(section.full, `${header}${inject}${section.body}`);
    }
    return next;
}

function buildIbInjection(component: TouchComponentAnalysis, varPrefix: string, st: string) {
    const id = token(component.id);
    const lines = `
 if $${varPrefix}_detect_allowed == 1
	run = CustomShader${st}Bake${id}
	run = CustomShader${st}Detect${id}
endif
if $${varPrefix}_mode == 1
	if time != $${varPrefix}_last_dispatch_${id}
		run = CustomShader${st}Jiggle${id}
		$${varPrefix}_last_dispatch_${id} = time
	endif
	vb0 = Resource${st}TempVB${id}
endif
`.trim();

    if (!component.variantCondition) return lines;
    return `if ${component.variantCondition}\n${lines
        .split("\n")
        .map((line) => `\t${line}`)
        .join("\n")}\nendif`;
}

function appendRuntime(text: string, varPrefix: string, entries: InteractiveEntry[]) {
    const st = sectionToken(varPrefix);
    if (text.includes(`[CommandList${st}Present]`)) return text;
    const zoneOverrides = buildTouchRuntimeZoneOverrides(entries.map((entry) => entry.draft));

    const lines = [
        "",
        `; ---- Nahida Touch Profile runtime (${varPrefix}) ----`,
        "",
        ...buildCursorAndPresent(varPrefix, st, entries),
        ...buildSharedShaders(varPrefix, st, entries, zoneOverrides),
        ...entries.flatMap((entry) => buildComponentShaders(varPrefix, st, entry, zoneOverrides)),
        ...buildSharedResources(st),
        ...entries.flatMap((entry) => buildComponentResources(st, entry)),
    ];

    return `${text.trimEnd()}\n${lines.join("\n")}\n`;
}

function buildCursorAndPresent(varPrefix: string, st: string, entries: InteractiveEntry[]) {
    const pinRuns = entries.map(
        (entry) => `run = CustomShader${st}Pin${token(entry.component.id)}`,
    );
    return [
        `[CommandList${st}Cursor]`,
        `$${varPrefix}_screen_w = 1`,
        `$${varPrefix}_screen_h = 1`,
        "if window_width > 0 && window_height > 0 && window_width <= 8192 && window_height <= 8192",
        `\t$${varPrefix}_screen_w = window_width`,
        `\t$${varPrefix}_screen_h = window_height`,
        "elif rt_width > 0 && rt_height > 0 && rt_width <= 8192 && rt_height <= 8192",
        `\t$${varPrefix}_screen_w = rt_width`,
        `\t$${varPrefix}_screen_h = rt_height`,
        "elif res_width > 0 && res_height > 0 && res_width <= 8192 && res_height <= 8192",
        `\t$${varPrefix}_screen_w = res_width`,
        `\t$${varPrefix}_screen_h = res_height`,
        "endif",
        "if cursor_x > 0 && cursor_y > 0 && cursor_x < 1 && cursor_y < 1",
        `\t$${varPrefix}_cursor_x = cursor_x * $${varPrefix}_screen_w`,
        `\t$${varPrefix}_cursor_y = (1.0 - cursor_y) * $${varPrefix}_screen_h`,
        "elif cursor_window_x > 0 && cursor_window_y > 0 && cursor_window_x < 1 && cursor_window_y < 1",
        `\t$${varPrefix}_cursor_x = cursor_window_x * $${varPrefix}_screen_w`,
        `\t$${varPrefix}_cursor_y = (1.0 - cursor_window_y) * $${varPrefix}_screen_h`,
        `elif cursor_screen_x >= 0 && cursor_screen_y >= 0 && cursor_screen_x <= $${varPrefix}_screen_w && cursor_screen_y <= $${varPrefix}_screen_h`,
        `\t$${varPrefix}_cursor_x = cursor_screen_x`,
        `\t$${varPrefix}_cursor_y = $${varPrefix}_screen_h - cursor_screen_y`,
        "else",
        `\t$${varPrefix}_cursor_x = -1`,
        `\t$${varPrefix}_cursor_y = -1`,
        "endif",
        `x24 = $${varPrefix}_cursor_x`,
        `y24 = $${varPrefix}_cursor_y`,
        `z24 = $${varPrefix}_screen_w`,
        `w24 = $${varPrefix}_screen_h`,
        "",
        `[CommandList${st}Present]`,
        `if $${varPrefix}_prev_time == 0`,
        `\t$${varPrefix}_delta_time = 0.0166667`,
        "else",
        `\t$${varPrefix}_delta_time = (time - $${varPrefix}_prev_time) * 60.0`,
        `\tif $${varPrefix}_delta_time > 0.100`,
        `\t\t$${varPrefix}_delta_time = 0.100`,
        `\telif $${varPrefix}_delta_time < 0.001`,
        `\t\t$${varPrefix}_delta_time = 0.001`,
        "\tendif",
        "endif",
        `$${varPrefix}_prev_time = time`,
        "",
        `$${varPrefix}_mouse_down = 0`,
        `$${varPrefix}_poke_sign = 0`,
        `if $${varPrefix}_modifier_down == 1`,
        `\tif ($${varPrefix}_lmb_down == 1 && $${varPrefix}_rmb_down == 1) || $${varPrefix}_x_down == 1`,
        `\t\t$${varPrefix}_mouse_down = 1`,
        "\tendif",
        "endif",
        `if $${varPrefix}_mouse_down == 1`,
        `\t$${varPrefix}_combo_active = 1`,
        "endif",
        "",
        `if $${varPrefix}_modifier_down == 1 && $${varPrefix}_combo_active == 0`,
        `\tif $${varPrefix}_lmb_prev == 1 && $${varPrefix}_lmb_down == 0`,
        `\t\t$${varPrefix}_poke_sign = -1`,
        `\t\t$${varPrefix}_poke_mult = time - $${varPrefix}_lmb_press_time`,
        `\telif $${varPrefix}_rmb_prev == 1 && $${varPrefix}_rmb_down == 0`,
        `\t\t$${varPrefix}_poke_sign = 1`,
        `\t\t$${varPrefix}_poke_mult = time - $${varPrefix}_rmb_press_time`,
        "\tendif",
        "endif",
        `if $${varPrefix}_poke_sign != 0`,
        `\tif $${varPrefix}_poke_mult < 0.25`,
        `\t\t$${varPrefix}_poke_mult = 0.25`,
        `\telif $${varPrefix}_poke_mult > 1.0`,
        `\t\t$${varPrefix}_poke_mult = 1.0`,
        "\tendif",
        "else",
        `\t$${varPrefix}_poke_mult = 1.0`,
        "endif",
        "",
        `$${varPrefix}_charge_sign = 0`,
        `if $${varPrefix}_modifier_down == 1 && $${varPrefix}_combo_active == 0`,
        `\tif $${varPrefix}_lmb_down == 1 && $${varPrefix}_rmb_down == 0`,
        `\t\t$${varPrefix}_charge_sign = -1`,
        `\telif $${varPrefix}_rmb_down == 1 && $${varPrefix}_lmb_down == 0`,
        `\t\t$${varPrefix}_charge_sign = 1`,
        "\tendif",
        "endif",
        `if $${varPrefix}_lmb_down == 1 && $${varPrefix}_lmb_prev == 0`,
        `\t$${varPrefix}_lmb_press_time = time`,
        "endif",
        `if $${varPrefix}_rmb_down == 1 && $${varPrefix}_rmb_prev == 0`,
        `\t$${varPrefix}_rmb_press_time = time`,
        "endif",
        `$${varPrefix}_lmb_prev = $${varPrefix}_lmb_down`,
        `$${varPrefix}_rmb_prev = $${varPrefix}_rmb_down`,
        `if $${varPrefix}_lmb_down == 0 && $${varPrefix}_rmb_down == 0 && $${varPrefix}_x_down == 0`,
        `\t$${varPrefix}_combo_active = 0`,
        "endif",
        "",
        `if $${varPrefix}_active == 1 && $${varPrefix}_mode == 1`,
        `\tif $${varPrefix}_initialized == 0`,
        `\t\trun = CustomShader${st}PinDetected`,
        ...pinRuns.map((line) => `\t\t${line}`),
        `\t\t$${varPrefix}_initialized = 1`,
        "\telse",
        `\t\trun = CustomShader${st}PinDetected`,
        ...pinRuns.map((line) => `\t\t${line}`),
        `\t\trun = CustomShader${st}UpdateScreenState`,
        "\tendif",
        `\t$${varPrefix}_detect_allowed = 1`,
        "else",
        `\t$${varPrefix}_detect_allowed = 0`,
        "endif",
        `run = CommandList${st}Cursor`,
        "",
    ];
}

function buildSharedShaders(
    varPrefix: string,
    st: string,
    entries: InteractiveEntry[],
    zoneOverrides: TouchRuntimeZoneOverrides,
) {
    const lines = [
        `[CustomShader${st}PinDetected]`,
        "cs = Resources/IM/rzm_pin_detected.hlsl",
        `x24 = $${varPrefix}_cursor_x`,
        `y24 = $${varPrefix}_cursor_y`,
        `z24 = $${varPrefix}_screen_w`,
        `w24 = $${varPrefix}_screen_h`,
        `cs-u0 = Resource${st}DetectID`,
        `cs-u1 = Resource${st}PinnedID`,
        `cs-u2 = Resource${st}PinnedInfo`,
        "dispatch = 1, 1, 1",
        "post cs-u0 = null",
        "post cs-u1 = null",
        "post cs-u2 = null",
        "",
    ];

    for (const entry of entries) {
        const id = token(entry.component.id);
        lines.push(
            `[CustomShader${st}Pin${id}]`,
            "cs = Resources/IM/rzm_pin_detected.hlsl",
            `cs-u0 = Resource${st}ComponentDetect${id}`,
            `cs-u1 = Resource${st}PinnedComponentID${id}`,
            `cs-u2 = Resource${st}PinnedComponentInfo${id}`,
            "dispatch = 1, 1, 1",
            "post cs-u0 = null",
            "post cs-u1 = null",
            "post cs-u2 = null",
            "",
        );
    }

    lines.push(
        `[CustomShader${st}UpdateScreenState]`,
        "local $cursor_x_past",
        "local $cursor_y_past",
        "local $was_mouse_down",
        `if $${varPrefix}_mouse_down == 1`,
        "\tif $was_mouse_down == 0",
        `\t\t$cursor_x_past = $${varPrefix}_cursor_x`,
        `\t\t$cursor_y_past = $${varPrefix}_cursor_y`,
        "\tendif",
        "\t$was_mouse_down = 1",
        "\tw67 = 1",
        "else",
        "\t$was_mouse_down = 0",
        "\t$cursor_x_past = 0",
        "\t$cursor_y_past = 0",
        "\tw67 = 0",
        "endif",
        "cs = Resources/IM/rzm_jiggle_screen_state.hlsl",
        "x67 = $cursor_x_past",
        "y67 = $cursor_y_past",
        ...buildBasePhysicsLines(),
        `x69 = $${varPrefix}_cursor_x`,
        `y69 = $${varPrefix}_cursor_y`,
        `z69 = $${varPrefix}_screen_w`,
        `w69 = $${varPrefix}_screen_h`,
        "x72 = 0",
        "y72 = 1.0",
        "z72 = 0.333333",
        "w72 = 0.333333",
        "x73 = 1.0",
        "y73 = 1.0",
        "z73 = 1.0",
        `x76 = $${varPrefix}_delta_time`,
        "y76 = 3.0",
        "z76 = 3.0",
        ...buildZoneOverrideLines(zoneOverrides),
        `x84 = $${varPrefix}_poke_sign`,
        `y84 = $${varPrefix}_poke_mult`,
        "z84 = 8.0",
        `w84 = $${varPrefix}_charge_sign`,
        "x99 = 1",
        "y99 = 1",
        "z99 = 1",
        "w99 = 1",
        "x100 = 1",
        "y100 = 1",
        "z100 = 1",
        "w100 = 1",
        "x112 = 1",
        "y112 = 1",
        "z112 = 1",
        "w112 = 1",
        `cs-t67 = Resource${st}PinnedInfo`,
        `cs-u0 = Resource${st}ScreenState`,
        `cs-u1 = Resource${st}PathProgress`,
        "dispatch = 1, 1, 1",
        "post cs-t67 = null",
        "post cs-u0 = null",
        "post cs-u1 = null",
        "",
    );

    return lines;
}

function buildComponentShaders(
    varPrefix: string,
    st: string,
    entry: InteractiveEntry,
    zoneOverrides: TouchRuntimeZoneOverrides,
) {
    const id = token(entry.component.id);
    const maskBase = maskResourceToken(entry.asset.assetPrefix);
    const paramsName = paramsResourceToken(entry.asset.assetPrefix);
    const primaryRange =
        entry.component.objectMaps.find((map) => map.label === "nude") ??
        entry.component.objectMaps[0] ??
        entry.component.drawRanges[0];
    const samples = bakeSampleOffsets(
        primaryRange?.firstIndex ?? 0,
        primaryRange?.indexCount ?? Math.max(entry.component.indexCount, 1),
    );

    const lines = [
        `[CustomShader${st}Bake${id}]`,
        "run = BuiltInCommandListUnbindAllRenderTargets",
        `clear = Resource${st}BakeRT 0.0`,
        ...samples.map((_, index) => `run = CustomShader${st}Bake${id}${index}`),
        "",
    ];

    samples.forEach((sample, index) => {
        lines.push(
            `[CustomShader${st}Bake${id}${index}]`,
            "gs = Resources/IM/rzm_gs_probe.hlsl",
            `gs-t1 = Resource${entry.component.indexResourceName}`,
            "ps = Resources/IM/rzm_gs_probe.hlsl",
            "topology = point_list",
            `o0 = set_viewport no_view_cache Resource${st}BakeRT`,
            `x26 = ${index}`,
            `y26 = ${sample}`,
            `drawindexed = 1, ${sample}, 0`,
            "",
        );
    });

    lines.push(
        `[CustomShader${st}Detect${id}]`,
        "cs = Resources/IM/rzm_object_detect.hlsl",
        `x28 = ${entry.draft.objectId}`,
        "cs-t0 = vb0",
        "cs-t1 = ib",
    );

    if (entry.asset.objectMapPaths.length >= 2 && entry.component.kind === "body") {
        const clothed =
            entry.asset.objectMapPaths.find((map) => /clothed/i.test(map.label)) ??
            entry.asset.objectMapPaths[0];
        const nude =
            entry.asset.objectMapPaths.find((map) => /nude/i.test(map.label)) ??
            entry.asset.objectMapPaths[1];
        lines.push(
            "if $body <= 1",
            `\tcs-t2 = Resource${st}${objectMapResourceToken(entry.asset.assetPrefix, clothed.label)}`,
            "else",
            `\tcs-t2 = Resource${st}${objectMapResourceToken(entry.asset.assetPrefix, nude.label)}`,
            "endif",
        );
    } else {
        lines.push(
            `cs-t2 = Resource${st}${objectMapResourceToken(entry.asset.assetPrefix, entry.asset.objectMapPaths[0]?.label ?? "main")}`,
        );
    }

    lines.push(
        `cs-t3 = Resource${st}BakeRT`,
        `cs-t4 = Resource${st}${maskBase}0`,
        `cs-t5 = Resource${st}${maskBase}1`,
        `cs-t7 = Resource${st}${maskBase}2`,
        `cs-t6 = Resource${st}ViewportAPI`,
        `cs-u0 = Resource${st}DetectID`,
        `cs-u1 = Resource${st}ComponentDetect${id}`,
        `cs-u2 = Resource${st}DebugDetect${id}`,
        `x24 = $${varPrefix}_cursor_x`,
        `y24 = $${varPrefix}_cursor_y`,
        `z24 = $${varPrefix}_screen_w`,
        `w24 = $${varPrefix}_screen_h`,
        `x25 = $${varPrefix}_mouse_down`,
        "x26 = 48.0",
        "w26 = 8.0",
        `x27 = $${varPrefix}_cursor_x`,
        `y27 = $${varPrefix}_cursor_y`,
        `z27 = $${varPrefix}_screen_w`,
        `w27 = $${varPrefix}_screen_h`,
        "x85 = 0",
        "y85 = 0",
        "z85 = 1",
        "w85 = 1",
        "x86 = 1",
        "x74 = 0",
        "dispatch = 1, 1, 1",
        "post cs-u0 = null",
        "post cs-u1 = null",
        "post cs-u2 = null",
        "",
        `[CustomShader${st}Jiggle${id}]`,
        "local $cursor_x_past",
        "local $cursor_y_past",
        "local $was_mouse_down",
        `if $${varPrefix}_mouse_down == 1`,
        "\tif $was_mouse_down == 0",
        `\t\t$cursor_x_past = $${varPrefix}_cursor_x`,
        `\t\t$cursor_y_past = $${varPrefix}_cursor_y`,
        "\tendif",
        "\t$was_mouse_down = 1",
        "\tw67 = 1",
        "else",
        "\t$was_mouse_down = 0",
        "\t$cursor_x_past = 0",
        "\t$cursor_y_past = 0",
        "\tw67 = 0",
        "endif",
        "cs = Resources/IM/rzm_jiggle_interaction.hlsl",
        "x67 = $cursor_x_past",
        "y67 = $cursor_y_past",
        ...buildBasePhysicsLines(),
        `x69 = $${varPrefix}_cursor_x`,
        `y69 = $${varPrefix}_cursor_y`,
        `z69 = $${varPrefix}_screen_w`,
        `w69 = $${varPrefix}_screen_h`,
        "x72 = 1",
        "y72 = 1.0",
        "z72 = 0.333333",
        "w72 = 0.333333",
        "x73 = 1.0",
        "y73 = 1.0",
        `x76 = $${varPrefix}_delta_time`,
        "y76 = 3.0",
        "z76 = 3.0",
        ...buildZoneOverrideLines(zoneOverrides),
        "x99 = 1",
        "y99 = 1",
        "z99 = 1",
        "w99 = 1",
        "x100 = 1",
        "y100 = 1",
        "z100 = 1",
        "w100 = 1",
        "x112 = 1",
        "y112 = 1",
        "z112 = 1",
        "w112 = 1",
        `cs-t67 = Resource${st}PinnedComponentInfo${id}`,
        `cs-t68 = Resource${st}${paramsName}`,
        `cs-t65 = Resource${st}${maskBase}0`,
        `cs-t66 = Resource${st}${maskBase}1`,
        `cs-t69 = Resource${st}${maskBase}2`,
        `cs-t71 = Resource${st}ScreenState`,
        `cs-t74 = Resource${st}PathProgress`,
        `cs-u6 = Resource${st}JiggleState${id}`,
        `Resource${st}TempVB${id} = vb0`,
        "cs-t24 = vb0",
        `cs-u5 = copy Resource${st}TempVB${id}`,
        `dispatch = (${entry.component.vertexCount} + 255) // 256, 1, 1`,
        "vb0 = null",
        `Resource${st}TempVB${id} = copy cs-u5`,
        "post cs-u5 = null",
        "post cs-u6 = null",
        "post cs-t71 = null",
        "",
    );

    return lines;
}

function buildSharedResources(st: string) {
    return [
        `[Resource${st}DetectID]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 15",
        "",
        `[Resource${st}PinnedID]`,
        "type = RWBuffer",
        "format = R32_FLOAT",
        "array = 1",
        "",
        `[Resource${st}PinnedInfo]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 15",
        "",
        `[Resource${st}ScreenState]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 15",
        "",
        `[Resource${st}PathProgress]`,
        "type = RWBuffer",
        "format = R32_FLOAT",
        "array = 12",
        "",
        `[Resource${st}ViewportAPI]`,
        "type = RWBuffer",
        "format = R32_FLOAT",
        "array = 16",
        "",
        `[Resource${st}BakeRT]`,
        "type = Texture2D",
        "mode = mono",
        "width = 8",
        "height = 2",
        "mips = 1",
        "array = 1",
        "msaa = 1",
        "msaa_quality = 0",
        "format = DXGI_FORMAT_R32G32B32A32_FLOAT",
        "bind_flags = render_target shader_resource",
        "",
    ];
}

function buildComponentResources(st: string, entry: InteractiveEntry) {
    const id = token(entry.component.id);
    const maskBase = maskResourceToken(entry.asset.assetPrefix);
    const paramsName = paramsResourceToken(entry.asset.assetPrefix);
    const lines: string[] = [];

    for (const objectMap of entry.asset.objectMapPaths) {
        lines.push(
            `[Resource${st}${objectMapResourceToken(entry.asset.assetPrefix, objectMap.label)}]`,
            "type = Buffer",
            "format = R32G32B32A32_FLOAT",
            `filename = ${objectMap.relativePath}`,
            "",
        );
    }

    entry.asset.maskPaths.forEach((maskPath, index) => {
        lines.push(
            `[Resource${st}${maskBase}${index}]`,
            "type = Buffer",
            "format = R32G32B32A32_FLOAT",
            `filename = ${maskPath}`,
            "",
        );
    });

    lines.push(
        `[Resource${st}${paramsName}]`,
        "type = Buffer",
        "format = R32G32B32A32_FLOAT",
        `filename = ${entry.asset.paramsRelativePath}`,
        "",
        `[Resource${st}ComponentDetect${id}]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 15",
        "",
        `[Resource${st}PinnedComponentID${id}]`,
        "type = RWBuffer",
        "format = R32_FLOAT",
        "array = 1",
        "",
        `[Resource${st}PinnedComponentInfo${id}]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 15",
        "",
        `[Resource${st}DebugDetect${id}]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 23",
        "",
        `[Resource${st}JiggleState${id}]`,
        "type = RWBuffer",
        "format = R32G32B32A32_FLOAT",
        "array = 10",
        "",
        `[Resource${st}TempVB${id}]`,
        "type = RWBuffer",
        "",
    );

    return lines;
}

export type TouchRuntimeZoneOverrides = {
    radius: number[];
    strength: number[];
    falloff: number[];
    maxOffset: number[];
    damping: number[];
    spring: number[];
};

export function buildTouchRuntimeZoneOverrides(
    drafts: TouchComponentDraft[],
): TouchRuntimeZoneOverrides {
    const overrides: TouchRuntimeZoneOverrides = {
        radius: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
        strength: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
        falloff: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
        maxOffset: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
        damping: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
        spring: Array.from({ length: TOUCH_ZONE_CHANNELS }, () => 0),
    };

    for (const draft of drafts) {
        for (const zone of draft.zones) {
            if (zone.channel < 0 || zone.channel >= TOUCH_ZONE_CHANNELS) {
                throw new Error(`Touch zone channel out of range: ${zone.channel}`);
            }

            const params = resolveTouchJiggleParams(zone.settings, draft.objectId);
            const values = {
                radius: params.radius,
                strength: params.strength,
                falloff: params.falloff,
                maxOffset: params.maxOffset,
                damping: params.grabDamping / DEFAULT_TOUCH_JIGGLE_PARAMS.grabDamping,
                spring: params.grabSpring / DEFAULT_TOUCH_JIGGLE_PARAMS.grabSpring,
            } satisfies Record<keyof TouchRuntimeZoneOverrides, number>;

            for (const key of Object.keys(values) as Array<keyof TouchRuntimeZoneOverrides>) {
                const current = overrides[key][zone.channel];
                const next = values[key];
                if (current !== 0 && Math.abs(current - next) > 0.000001) {
                    throw new Error(
                        `Touch zone channel ${zone.channel} has conflicting ${key} overrides`,
                    );
                }
                overrides[key][zone.channel] = next;
            }
        }
    }

    return overrides;
}

function buildBasePhysicsLines() {
    return [
        `x68 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.radius)}`,
        `y68 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.strength)}`,
        `z68 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.falloff)}`,
        `w68 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.dragScale)}`,
        `x70 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.grabDamping)}`,
        `y70 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.grabSpring)}`,
        `z70 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.releaseDamping)}`,
        `w70 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.releaseSpring)}`,
        `x71 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.maxOffset)}`,
        `y71 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.releaseKick)}`,
        `z71 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.mouseYDirection)}`,
        `w71 = ${formatIniNumber(DEFAULT_TOUCH_JIGGLE_PARAMS.targetFollow)}`,
    ];
}

function buildZoneOverrideLines(overrides: TouchRuntimeZoneOverrides) {
    return [
        ...buildPackedVectorLines(77, 78, 103, overrides.radius),
        ...buildPackedVectorLines(79, 80, 106, overrides.strength),
        ...buildPackedVectorLines(81, 82, 109, overrides.maxOffset),
        ...buildPackedVectorLines(101, 102, 116, overrides.falloff),
        ...buildPackedVectorLines(122, 123, 124, overrides.damping),
        ...buildPackedVectorLines(125, 126, 127, overrides.spring),
    ];
}

function buildPackedVectorLines(low: number, high: number, r2: number, values: number[]) {
    return [
        ...packedVectorLines(low, values.slice(0, 4)),
        ...packedVectorLines(high, values.slice(4, 8)),
        ...packedVectorLines(r2, values.slice(8, 12)),
    ];
}

function packedVectorLines(slot: number, values: number[]) {
    return [
        `x${slot} = ${formatIniNumber(values[0] ?? 0)}`,
        `y${slot} = ${formatIniNumber(values[1] ?? 0)}`,
        `z${slot} = ${formatIniNumber(values[2] ?? 0)}`,
        `w${slot} = ${formatIniNumber(values[3] ?? 0)}`,
    ];
}

function formatIniNumber(value: number) {
    return Number(value.toFixed(6)).toString();
}

function matchSection(text: string, header: string) {
    const escaped = header.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const match = text.match(new RegExp(`${escaped}([^\\[]*)`, "i"));
    if (!match) return null;
    return { full: match[0], body: match[1] };
}

function token(value: string) {
    return value.replace(/[^a-zA-Z0-9]+/g, "");
}

function sectionToken(varPrefix: string) {
    return varPrefix
        .split(/[^a-zA-Z0-9]+/)
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join("");
}

function componentKindToken(assetPrefix: string) {
    const match = assetPrefix.match(/(Body|Leg|Hair|Mesh)$/i);
    if (match) return match[1].charAt(0).toUpperCase() + match[1].slice(1).toLowerCase();
    return assetPrefix.replace(/[^a-zA-Z0-9]/g, "");
}

function maskResourceToken(assetPrefix: string) {
    return `${componentKindToken(assetPrefix)}Masks`;
}

function paramsResourceToken(assetPrefix: string) {
    return `${componentKindToken(assetPrefix)}Params`;
}

function objectMapResourceToken(assetPrefix: string, label: string) {
    const kind = componentKindToken(assetPrefix);
    if (label === "main" || label === "skin") return `${kind}ObjectMap`;
    return `${kind}${label.charAt(0).toUpperCase()}${label.slice(1)}ObjectMap`;
}
