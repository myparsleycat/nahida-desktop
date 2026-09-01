/**
 * Portions of the Menu Maker model are derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
import type {
    MenuMakerGeometry,
    MenuMakerSettings as BoundMenuMakerSettings,
    MenuMakerSlot as BoundMenuMakerSlot,
} from "@bindings/menumaker";

export type MenuMakerKeyType = "cycle" | "toggle" | "hold" | "activate";

export type MenuMakerIcon =
    | { kind: "lucide"; name: string; color: string }
    | { kind: "iconify"; name: string; color: string; svg: string }
    | { kind: "upload"; name: string; color: string; dataUrl: string };

export type MenuMakerSlot = BoundMenuMakerSlot & { icon: MenuMakerIcon };

export type MenuMakerSettings = BoundMenuMakerSettings & {
    clickModifier: "alt" | "ctrl" | "shift" | "none";
    slotAlignment: "left" | "center" | "right";
    fallbackType: MenuMakerKeyType;
    panelImageDataUrl?: string;
};

export const MENU_MAKER_BASE_SLOT_SIZE = 64;
export const MENU_MAKER_BASE_PANEL_IMAGE_SIZE = 256;

export function menuMakerTitleText(settings: Pick<MenuMakerSettings, "title">): string {
    return settings.title.trim();
}

export const DEFAULT_MENU_MAKER_SETTINGS: MenuMakerSettings = {
    title: "",
    menuKey: "alt",
    clickModifier: "alt",
    columns: 3,
    gap: 14,
    baseWidth: 1920,
    baseHeight: 1080,
    panelScale: 1,
    slotAlignment: "center",
    fallbackType: "cycle",
    removeOriginalKeys: false,
    showKeyHint: true,
    hideUploadLabel: true,
    useOriginalININame: true,
    resetActiveOnPresent: false,
    palette: {
        accent: "#ff4fb3",
        panelBackground: "#11131a",
        panelBackgroundAlpha: 210,
        panelBorder: "#ff4fb3",
        panelBorderAlpha: 255,
        slotBackground: "#1c2030",
        slotBackgroundAlpha: 200,
        slotHover: "#5d2850",
        slotHoverAlpha: 230,
        slotBorder: "#4a5068",
        slotBorderAlpha: 255,
        title: "#ffffff",
        titleShadow: "#000000",
    },
};

export function emptyMenuMakerGeometry(): MenuMakerGeometry {
    return {
        panelWidth: 0,
        panelHeight: 0,
        slotSize: MENU_MAKER_BASE_SLOT_SIZE,
        padding: 16,
        titleHeight: 0,
        scaledGap: 0,
        slots: [],
    };
}
