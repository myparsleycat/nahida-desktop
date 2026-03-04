export function formatKeyLabel(key: string, options?: { asciiFallback?: boolean }) {
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

        VK_UP: options?.asciiFallback ? "Up" : "↑",
        UP: options?.asciiFallback ? "Up" : "↑",
        VK_DOWN: options?.asciiFallback ? "Down" : "↓",
        DOWN: options?.asciiFallback ? "Down" : "↓",
        VK_LEFT: options?.asciiFallback ? "Left" : "←",
        LEFT: options?.asciiFallback ? "Left" : "←",
        VK_RIGHT: options?.asciiFallback ? "Right" : "→",
        RIGHT: options?.asciiFallback ? "Right" : "→",
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
        XB_DPAD_UP: options?.asciiFallback ? "D-Pad Up" : "D-Pad ↑",
        XB_DPAD_DOWN: options?.asciiFallback ? "D-Pad Down" : "D-Pad ↓",
        XB_DPAD_LEFT: options?.asciiFallback ? "D-Pad Left" : "D-Pad ←",
        XB_DPAD_RIGHT: options?.asciiFallback ? "D-Pad Right" : "D-Pad →",
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

export function formatKeySequence(
    keyString: string,
    options?: { asciiFallback?: boolean; separator?: string },
): string {
    if (!keyString) return "";
    return keyString
        .split(" ")
        .map((k) => formatKeyLabel(k, { asciiFallback: options?.asciiFallback }))
        .filter((k) => k !== null)
        .join(options?.separator ?? " + ");
}

export function getBaseKey(keyString: string): string | null {
    if (!keyString) return null;
    const parts = keyString.trim().split(/\s+/).filter(Boolean);
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
            const parts = keyStr.trim().toLowerCase().split(/\s+/).filter(Boolean);
            if (parts.includes("ctrl")) used.ctrl = true;
            if (parts.includes("alt")) used.alt = true;
            if (parts.includes("shift")) used.shift = true;
        }
    }
    return used;
}
