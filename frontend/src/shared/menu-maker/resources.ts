/**
 * Resource output is derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
import type {
    MenuMakerGeneratedAsset,
    MenuMakerGeometry,
    MenuMakerSlotStateGroup,
    MenuMakerSlotValueState,
} from "@bindings/menumaker";
import { dynamicIconImports } from "lucide-react/dynamic";
import { createElement, type ComponentType } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import {
    MENU_MAKER_BASE_SLOT_SIZE,
    menuMakerTitleText,
    type MenuMakerSettings,
    type MenuMakerSlot,
} from "./types";

export const MENU_MAKER_SHADER = `// **** RESPONSIVE UI SHADER ****
// Contributors: SinsOfSeven
// Inspired by VV_Mod_Maker

Texture1D<float4> IniParams : register(t120);

#define SIZE   IniParams[87].xy
#define OFFSET IniParams[87].zw

struct vs2ps {
\tfloat4 pos : SV_Position0;
\tfloat2 uv  : TEXCOORD1;
};

#ifdef VERTEX_SHADER
void main(out vs2ps output, uint vertex : SV_VertexID)
{
\tfloat2 BaseCoord, Offset;
\tOffset.x = OFFSET.x * 2 - 1;
\tOffset.y = (1 - OFFSET.y) * 2 - 1;
\tBaseCoord.xy = float2(2 * SIZE.x, 2 * (-SIZE.y));
\tswitch (vertex) {
\t\tcase 0: output.pos.xy = float2(BaseCoord.x + Offset.x, BaseCoord.y + Offset.y); output.uv = float2(1, 0); break;
\t\tcase 1: output.pos.xy = float2(BaseCoord.x + Offset.x, 0           + Offset.y); output.uv = float2(1, 1); break;
\t\tcase 2: output.pos.xy = float2(0           + Offset.x, BaseCoord.y + Offset.y); output.uv = float2(0, 0); break;
\t\tcase 3: output.pos.xy = float2(0           + Offset.x, 0           + Offset.y); output.uv = float2(0, 1); break;
\t\tdefault: output.pos.xy = 0; output.uv = float2(0, 0); break;
\t};
\toutput.pos.zw = float2(0, 1);
}
#endif

#ifdef PIXEL_SHADER
Texture2D<float4> tex : register(t100);
void main(vs2ps input, out float4 result : SV_Target0)
{
\tuint width, height;
\ttex.GetDimensions(width, height);
\tif (!width || !height) discard;
\tinput.uv.y = 1 - input.uv.y;
\tresult = tex.Load(int3(input.uv.xy * float2(width, height), 0));
}
#endif
`;

export async function renderMenuMakerAssets(
    slots: MenuMakerSlot[],
    settings: MenuMakerSettings,
    geometry: MenuMakerGeometry,
    slotStates: MenuMakerSlotStateGroup[] | null | undefined,
): Promise<MenuMakerGeneratedAsset[]> {
    await loadInterFont();
    const activeSlots = slots.filter((slot) => !slot.skip);
    const files: MenuMakerGeneratedAsset[] = [
        {
            relativePath: "res_gui/draw_2d.hlsl",
            data: bytesToBase64(new TextEncoder().encode(MENU_MAKER_SHADER)),
        },
    ];

    const background = makeCanvas(geometry.panelWidth, geometry.panelHeight);
    const backgroundContext = requireContext(background);
    backgroundContext.clearRect(0, 0, background.width, background.height);
    backgroundContext.fillStyle = rgba(
        settings.palette.panelBackground,
        settings.palette.panelBackgroundAlpha,
    );
    backgroundContext.fillRect(0, 0, background.width, background.height);
    if (settings.panelImageDataUrl) {
        const image = await loadImage(settings.panelImageDataUrl);
        backgroundContext.globalAlpha = settings.palette.panelBackgroundAlpha / 255;
        backgroundContext.drawImage(image, 0, 0, background.width, background.height);
        backgroundContext.globalAlpha = 1;
    }
    backgroundContext.strokeStyle = rgba(
        settings.palette.panelBorder,
        settings.palette.panelBorderAlpha,
    );
    backgroundContext.lineWidth = Math.max(1, Math.round(2 * settings.panelScale));
    backgroundContext.strokeRect(1, 1, background.width - 2, background.height - 2);
    files.push({
        relativePath: "res_gui/bg.png",
        data: bytesToBase64(await canvasBytes(background)),
    });

    const titleText = menuMakerTitleText(settings);
    if (titleText && geometry.titleHeight) {
        const title = makeCanvas(
            Math.max(1, geometry.panelWidth - Math.round(6 * settings.panelScale)),
            geometry.titleHeight,
        );
        const titleContext = requireContext(title);
        titleContext.font = `600 ${Math.max(12, Math.round(18 * settings.panelScale))}px "Nahida Menu Maker Inter"`;
        titleContext.textBaseline = "middle";
        titleContext.shadowColor = settings.palette.titleShadow;
        titleContext.shadowBlur = Math.round(3 * settings.panelScale);
        titleContext.fillStyle = settings.palette.title;
        titleContext.fillText(titleText, Math.round(10 * settings.panelScale), title.height / 2);
        files.push({
            relativePath: "res_gui/title.png",
            data: bytesToBase64(await canvasBytes(title)),
        });
    }

    for (const [index, slot] of activeSlots.entries()) {
        const suffix = String(index + 1).padStart(2, "0");
        const states = statesForSlot(slot.id, slotStates);
        const variants = states.length
            ? states
            : [{ value: "", active: false, fileSuffix: "", resourceSuffix: "", variable: "" }];
        for (const state of variants) {
            files.push({
                relativePath: `res_gui/slot_${suffix}${state.fileSuffix}.png`,
                data: bytesToBase64(
                    await renderSlot(
                        slot,
                        settings,
                        geometry.slotSize,
                        false,
                        state.active,
                        state.value,
                    ),
                ),
            });
            files.push({
                relativePath: `res_gui/slot_hover_${suffix}${state.fileSuffix}.png`,
                data: bytesToBase64(
                    await renderSlot(
                        slot,
                        settings,
                        geometry.slotSize,
                        true,
                        state.active,
                        state.value,
                    ),
                ),
            });
        }
    }
    return files;
}

async function renderSlot(
    slot: MenuMakerSlot,
    settings: MenuMakerSettings,
    slotSize: number,
    hover: boolean,
    active = false,
    valueLabel = "",
): Promise<Uint8Array> {
    const px = (value: number) => Math.round((value * slotSize) / MENU_MAKER_BASE_SLOT_SIZE);
    const canvas = makeCanvas(slotSize, slotSize);
    const context = requireContext(canvas);
    context.fillStyle =
        hover || active
            ? rgba(settings.palette.slotHover, settings.palette.slotHoverAlpha)
            : rgba(settings.palette.slotBackground, settings.palette.slotBackgroundAlpha);
    context.fillRect(0, 0, slotSize, slotSize);
    context.strokeStyle = active
        ? settings.palette.accent
        : rgba(settings.palette.slotBorder, settings.palette.slotBorderAlpha);
    context.lineWidth = Math.max(1, px(active && hover ? 3 : 2));
    context.strokeRect(px(1), px(1), slotSize - px(2), slotSize - px(2));
    const lucideSize = Math.max(1, px(36));
    if (slot.icon.kind === "upload") {
        context.drawImage(
            await loadImage(slot.icon.dataUrl),
            px(12),
            px(8),
            Math.max(1, px(40)),
            Math.max(1, px(40)),
        );
    } else if (slot.icon.kind === "iconify") {
        context.drawImage(
            await loadImage(
                `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svgWithPixelSize(slot.icon.svg, lucideSize))}`,
            ),
            px(14),
            px(8),
            lucideSize,
            lucideSize,
        );
    } else {
        const loader = dynamicIconImports[slot.icon.name as keyof typeof dynamicIconImports];
        if (loader) {
            const icon = (await loader()).default as ComponentType<Record<string, unknown>>;
            const svg = renderToStaticMarkup(
                createElement(icon, {
                    xmlns: "http://www.w3.org/2000/svg",
                    width: lucideSize,
                    height: lucideSize,
                    color: slot.icon.color,
                    strokeWidth: 2,
                }),
            );
            context.drawImage(
                await loadImage(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`),
                px(14),
                px(8),
                lucideSize,
                lucideSize,
            );
        } else {
            context.fillStyle = slot.icon.color;
            context.font = `700 ${Math.max(8, px(22))}px "Nahida Menu Maker Inter"`;
            context.textAlign = "center";
            context.textBaseline = "middle";
            context.fillText(slot.name.slice(0, 1).toUpperCase(), px(32), px(27));
        }
    }
    context.shadowColor = "#000000";
    context.shadowBlur = px(2);
    context.fillStyle = "#ffffff";
    context.font = `500 ${Math.max(6, px(8))}px "Nahida Menu Maker Inter"`;
    context.textAlign = settings.slotAlignment;
    context.textBaseline = "alphabetic";
    const label =
        slot.icon.kind === "upload" && settings.hideUploadLabel ? "" : truncate(slot.name, 14);
    const labelX =
        settings.slotAlignment === "left"
            ? px(5)
            : settings.slotAlignment === "right"
              ? slotSize - px(5)
              : px(32);
    if (label) context.fillText(label, labelX, settings.showKeyHint ? px(52) : px(58));
    if (settings.showKeyHint) {
        context.fillStyle = settings.palette.accent;
        context.font = `500 ${Math.max(5, px(7))}px "Nahida Menu Maker Inter"`;
        context.fillText(truncate(slot.key, 16), labelX, px(61));
    }
    if (valueLabel) {
        context.shadowBlur = 0;
        context.fillStyle = settings.palette.accent;
        context.font = `700 ${Math.max(7, px(11))}px "Nahida Menu Maker Inter"`;
        context.textAlign = "right";
        context.textBaseline = "top";
        context.fillText(truncate(valueLabel, 5), slotSize - px(4), px(3));
    }
    return canvasBytes(canvas);
}

function statesForSlot(
    slotId: string,
    groups: MenuMakerSlotStateGroup[] | null | undefined,
): MenuMakerSlotValueState[] {
    return groups?.find((group) => group.slotId === slotId)?.states ?? [];
}

async function loadInterFont(): Promise<void> {
    const font = new FontFace("Nahida Menu Maker Inter", "url(/Inter-Medium.ttf)", {
        weight: "500 700",
    });
    try {
        await font.load();
        document.fonts.add(font);
        await document.fonts.ready;
        if (!document.fonts.check('12px "Nahida Menu Maker Inter"'))
            throw new Error("font check failed");
    } catch (error) {
        throw new Error("MENU_MAKER_INTER_FONT_LOAD_FAILED", { cause: error });
    }
}

function makeCanvas(width: number, height: number): HTMLCanvasElement {
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(width));
    canvas.height = Math.max(1, Math.round(height));
    return canvas;
}

function requireContext(canvas: HTMLCanvasElement): CanvasRenderingContext2D {
    const context = canvas.getContext("2d", { alpha: true });
    if (!context) throw new Error("MENU_MAKER_CANVAS_UNAVAILABLE");
    return context;
}

async function canvasBytes(canvas: HTMLCanvasElement): Promise<Uint8Array> {
    const blob = await new Promise<Blob>((resolve, reject) =>
        canvas.toBlob(
            (value) => (value ? resolve(value) : reject(new Error("MENU_MAKER_PNG_ENCODE_FAILED"))),
            "image/png",
        ),
    );
    return new Uint8Array(await blob.arrayBuffer());
}

async function loadImage(source: string): Promise<HTMLImageElement> {
    const image = new Image();
    image.decoding = "async";
    image.src = source;
    await image.decode();
    return image;
}

function rgba(hex: string, alpha: number): string {
    const value = hex.replace("#", "");
    const normalized =
        value.length === 3
            ? `${value[0]}${value[0]}${value[1]}${value[1]}${value[2]}${value[2]}`
            : value;
    const number = Number.parseInt(normalized, 16);
    return `rgba(${(number >> 16) & 255}, ${(number >> 8) & 255}, ${number & 255}, ${Math.max(0, Math.min(255, alpha)) / 255})`;
}

function truncate(value: string, length: number): string {
    return value.length > length ? `${value.slice(0, length - 1)}…` : value;
}

function svgWithPixelSize(svg: string, size: number): string {
    const withoutSize = svg.replace(/\s(?:width|height)\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)/gi, "");
    return withoutSize.replace(/<svg\b/i, `<svg width="${size}" height="${size}"`);
}

function bytesToBase64(bytes: Uint8Array): string {
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += 0x8000) {
        binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
    }
    return btoa(binary);
}
