import type { MenuMakerDocument, MenuMakerScanResult, MenuMakerSource } from "@bindings/menumaker";
import { withSuggestedIcons } from "@shared/menu-maker/parser";
import { type MenuMakerSettings, type MenuMakerSlot } from "@shared/menu-maker/types";

export interface EditorState {
    source?: MenuMakerSource;
    document?: MenuMakerDocument;
    slots: MenuMakerSlot[];
    settings: MenuMakerSettings;
    scan?: MenuMakerScanResult;
    sourceAvailable: boolean;
    busy: boolean;
    revision: number;
}

export type EditorAction =
    | { type: "busy"; value: boolean }
    | { type: "scan"; value: MenuMakerScanResult }
    | {
          type: "load";
          source: MenuMakerSource;
          document: MenuMakerDocument;
          slots?: MenuMakerSlot[];
          settings?: MenuMakerSettings;
          sourceAvailable?: boolean;
      }
    | { type: "sourceAvailable"; value: boolean }
    | { type: "sourceContent"; text: string; sha256: string }
    | { type: "slots"; value: MenuMakerSlot[] }
    | { type: "settings"; value: Partial<MenuMakerSettings> }
    | { type: "palette"; key: keyof MenuMakerSettings["palette"]; value: string | number };

export function reducer(state: EditorState, action: EditorAction): EditorState {
    if (action.type === "busy") return { ...state, busy: action.value };
    if (action.type === "sourceAvailable") return { ...state, sourceAvailable: action.value };
    if (action.type === "sourceContent") {
        if (!state.source) return state;
        return {
            ...state,
            source: { ...state.source, text: action.text, sha256: action.sha256 },
        };
    }
    if (action.type === "scan") return { ...state, scan: action.value };
    if (action.type === "load")
        return {
            ...state,
            source: action.source,
            document: action.document,
            slots: action.slots ?? withSuggestedIcons(action.document.slots),
            settings: action.settings ?? state.settings,
            sourceAvailable: action.sourceAvailable ?? true,
            revision: state.revision + 1,
        };
    if (action.type === "slots")
        return { ...state, slots: action.value, revision: state.revision + 1 };
    if (action.type === "settings")
        return {
            ...state,
            settings: { ...state.settings, ...action.value },
            revision: state.revision + 1,
        };
    return {
        ...state,
        settings: {
            ...state.settings,
            palette: { ...state.settings.palette, [action.key]: action.value },
        },
        revision: state.revision + 1,
    };
}
