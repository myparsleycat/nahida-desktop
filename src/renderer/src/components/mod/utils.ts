export function formatKeyLabel(key: string) {
    if (!key) return null;

    const lowerKey = key.trim().toLowerCase();

    if (lowerKey.startsWith("no_")) {
        return null;
    }

    const upperKey = key.trim().toUpperCase();

    const keyMap: Record<string, string> = {
        VK_CONTROL: "Ctrl",
        CONTROL: "Ctrl",
        VK_LCONTROL: "Ctrl",
        VK_RCONTROL: "Ctrl",
        VK_MENU: "Alt",
        ALT: "Alt",
        VK_LMENU: "Alt",
        VK_RMENU: "Alt",
        VK_SHIFT: "Shift",
        SHIFT: "Shift",
        VK_LSHIFT: "Shift",
        VK_RSHIFT: "Shift",
        VK_LWIN: "Win",
        VK_RWIN: "Win",
        VK_APPS: "Menu",

        VK_UP: "↑",
        UP: "↑",
        VK_DOWN: "↓",
        DOWN: "↓",
        VK_LEFT: "←",
        LEFT: "←",
        VK_RIGHT: "→",
        RIGHT: "→",
        VK_HOME: "Home",
        VK_END: "End",
        VK_PRIOR: "PgUp",
        VK_NEXT: "PgDn",

        VK_RETURN: "Enter",
        ENTER: "Enter",
        VK_BACK: "Backspace",
        VK_TAB: "Tab",
        VK_SPACE: "Space",
        VK_ESCAPE: "Esc",
        VK_DELETE: "Del",
        VK_INSERT: "Ins",
        VK_SNAPSHOT: "PrtSc",
        VK_PAUSE: "Pause",

        VK_OEM_3: "`",
        VK_OEM_MINUS: "-",
        VK_OEM_PLUS: "+",
        VK_OEM_COMMA: ",",
        VK_OEM_PERIOD: ".",
        VK_OEM_1: ";",
        VK_OEM_2: "/",
        VK_OEM_4: "[",
        VK_OEM_5: "\\",
        VK_OEM_6: "]",
        VK_OEM_7: "'",

        XB_LEFT_TRIGGER: "LT",
        XB_RIGHT_TRIGGER: "RT",
        XB_LEFT_SHOULDER: "LB",
        XB_RIGHT_SHOULDER: "RB",
        XB_LEFT_THUMB: "LS",
        XB_RIGHT_THUMB: "RS",
        XB_DPAD_UP: "D-Pad ↑",
        XB_DPAD_DOWN: "D-Pad ↓",
        XB_DPAD_LEFT: "D-Pad ←",
        XB_DPAD_RIGHT: "D-Pad →",
        XB_A: "A",
        XB_B: "B",
        XB_X: "X",
        XB_Y: "Y",
        XB_START: "Start",
        XB_BACK: "Back",
        XB_GUIDE: "Guide",
    };

    if (keyMap[upperKey]) {
        return keyMap[upperKey];
    }

    if (upperKey.startsWith("VK_")) {
        const stripped = upperKey.replace("VK_", "");

        if (stripped.length === 1 || /^F\d+$/.test(stripped)) {
            return stripped;
        }
        if (stripped.startsWith("NUMPAD")) {
            return stripped.replace("NUMPAD", "Num");
        }
    }

    if (upperKey.startsWith("XB_")) {
        return upperKey.replace("XB_", "");
    }

    return key.charAt(0).toUpperCase() + key.slice(1);
}

export function getBaseKey(keyString: string): string | null {
    if (!keyString) return null;
    const parts = keyString.split(" ");
    const modifiers = new Set([
        "ctrl",
        "alt",
        "shift",
        "win",
        "no_ctrl",
        "no_alt",
        "no_shift",
        "no_win",
    ]);
    for (const part of parts) {
        if (!modifiers.has(part.toLowerCase())) {
            const lowerPart = part.toLowerCase();
            if (lowerPart === "up") return "vk_up";
            if (lowerPart === "down") return "vk_down";
            if (lowerPart === "left") return "vk_left";
            if (lowerPart === "right") return "vk_right";
            return lowerPart;
        }
    }
    return null;
}

export function getUsedModifiers(baseKey: string, otherKeys: string[]) {
    const used = { ctrl: false, alt: false, shift: false };
    if (!baseKey) return used;

    const targetBase = baseKey.toLowerCase();

    for (const keyStr of otherKeys) {
        if (!keyStr) continue;
        const currentBase = getBaseKey(keyStr);
        if (currentBase && currentBase.toLowerCase() === targetBase) {
            const parts = keyStr.toLowerCase().split(" ");
            if (parts.includes("ctrl")) used.ctrl = true;
            if (parts.includes("alt")) used.alt = true;
            if (parts.includes("shift")) used.shift = true;
        }
    }
    return used;
}

export function mapKeyboardEventToInternal(
    e: React.KeyboardEvent | KeyboardEvent,
    strictMods?: { ctrl?: boolean; alt?: boolean; shift?: boolean },
): string | null {
    const key = e.key;

    if (key === "Control" || key === "Alt" || key === "Shift" || key === "Meta") {
        return null;
    }

    // const simpleCtrl = e.ctrlKey ? "ctrl" : "";
    // const simpleAlt = e.altKey ? "alt" : "";
    // const simpleShift = e.shiftKey ? "shift" : "";

    let mappedKey = key.toLowerCase();
    const code = e.code;

    const specialKeys: Record<string, string> = {
        ArrowUp: "vk_up",
        ArrowDown: "vk_down",
        ArrowLeft: "vk_left",
        ArrowRight: "vk_right",
        Enter: "vk_return",
        Space: "vk_space",
        Tab: "vk_tab",
        Escape: "vk_escape",
        Backspace: "vk_back",
        Delete: "vk_delete",
        Insert: "vk_insert",
        Home: "vk_home",
        End: "vk_end",
        PageUp: "vk_prior",
        PageDown: "vk_next",
        Pause: "vk_pause",
        PrintScreen: "vk_snapshot",
        ContextMenu: "vk_apps",
        F1: "vk_f1",
        F2: "vk_f2",
        F3: "vk_f3",
        F4: "vk_f4",
        F5: "vk_f5",
        F6: "vk_f6",
        F7: "vk_f7",
        F8: "vk_f8",
        F9: "vk_f9",
        F10: "vk_f10",
        F11: "vk_f11",
        F12: "vk_f12",
        Numpad0: "vk_numpad0",
        Numpad1: "vk_numpad1",
        Numpad2: "vk_numpad2",
        Numpad3: "vk_numpad3",
        Numpad4: "vk_numpad4",
        Numpad5: "vk_numpad5",
        Numpad6: "vk_numpad6",
        Numpad7: "vk_numpad7",
        Numpad8: "vk_numpad8",
        Numpad9: "vk_numpad9",
        NumpadMultiply: "vk_multiply",
        NumpadAdd: "vk_add",
        NumpadSubtract: "vk_subtract",
        NumpadDecimal: "vk_decimal",
        NumpadDivide: "vk_divide",
    };

    if (specialKeys[key]) {
        mappedKey = specialKeys[key];
    } else if (code.startsWith("Key")) {
        mappedKey = code.slice(3).toLowerCase();
    } else if (code.startsWith("Digit")) {
        mappedKey = code.slice(5);
    } else if (code === "BracketLeft") {
        mappedKey = "[";
    } else if (code === "BracketRight") {
        mappedKey = "]";
    } else if (code === "Backslash") {
        mappedKey = "\\";
    } else if (code === "Semicolon") {
        mappedKey = ";";
    } else if (code === "Quote") {
        mappedKey = "'";
    } else if (code === "Comma") {
        mappedKey = ",";
    } else if (code === "Period") {
        mappedKey = ".";
    } else if (code === "Slash") {
        mappedKey = "/";
    } else if (code === "Backquote") {
        mappedKey = "`";
    } else if (code === "Minus") {
        mappedKey = "-";
    } else if (code === "Equal") {
        mappedKey = "="; // + is Shift+=
    }

    if (mappedKey === "=") {
        mappedKey = "+";
    }

    const ctrl = e.ctrlKey ? "ctrl" : strictMods?.ctrl ? "no_ctrl" : "";
    const alt = e.altKey ? "alt" : strictMods?.alt ? "no_alt" : "";
    const shift = e.shiftKey ? "shift" : strictMods?.shift ? "no_shift" : "";

    return [ctrl, alt, shift, mappedKey].filter(Boolean).join(" ");
}

export const getModColorClass = (isEnabled: boolean) => {
    if (isEnabled) {
        return "dark:bg-[#0d430d] bg-[#6aad6a]";
    } else {
        return "dark:bg-[#58151b] bg-[#f1afb4]";
    }
};

export const getToggleBoxColorClass = (isEnabled: boolean) => {
    if (isEnabled) {
        return "dark:bg-[#0f4d0f]/80 bg-[#72b172]/80";
    } else {
        return "dark:bg-[#612127]/80 bg-[#f2b3b8]/80";
    }
};

export const getToggleInputColorClass = (isEnabled: boolean) => {
    if (isEnabled) {
        return "dark:bg-[#115a11] bg-[#d5e8d5]";
    } else {
        return "dark:bg-[#6a2e34] bg-[#fbe8ea]";
    }
};
