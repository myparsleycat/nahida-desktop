type OpenExternal = (url: string) => Promise<unknown>;

interface WindowTarget {
    location: { href: string };
    open: typeof window.open;
}

interface DocumentTarget {
    addEventListener(type: "click", listener: (event: MouseEvent) => void, capture: boolean): void;
    removeEventListener(
        type: "click",
        listener: (event: MouseEvent) => void,
        capture: boolean,
    ): void;
}

interface AnchorTarget {
    tagName: string;
    getAttribute(name: string): string | null;
    hasAttribute(name: string): boolean;
}

function isAnchorTarget(value: EventTarget): value is EventTarget & AnchorTarget {
    if (!value || typeof value !== "object") {
        return false;
    }
    const target = value as Partial<AnchorTarget>;
    return (
        target.tagName?.toLowerCase() === "a" &&
        typeof target.getAttribute === "function" &&
        typeof target.hasAttribute === "function"
    );
}

function resolveURL(value: string | URL | undefined, baseURL: string) {
    const target = value == null || value === "" ? "about:blank" : String(value);
    try {
        return new URL(target, baseURL).href;
    } catch {
        return target;
    }
}

function newContextAnchor(event: MouseEvent) {
    if (event.defaultPrevented || event.button !== 0) {
        return null;
    }

    const anchor = event.composedPath().find(isAnchorTarget);
    if (!anchor || anchor.hasAttribute("download")) {
        return null;
    }

    const target = anchor.getAttribute("target")?.trim().toLowerCase();
    if (!target || target === "_self" || target === "_parent" || target === "_top") {
        return null;
    }
    return anchor;
}

/**
 * Mirrors Electron's setWindowOpenHandler contract: every new browsing-context
 * request is denied in the WebView and forwarded to the operating system.
 */
export function installExternalWindowHandler(
    openExternal: OpenExternal,
    runtimeWindow: WindowTarget = window,
    runtimeDocument: DocumentTarget = document,
) {
    const originalOpen = runtimeWindow.open;
    const open = (value: string | URL | undefined) => {
        void openExternal(resolveURL(value, runtimeWindow.location.href));
    };

    const patchedOpen: typeof window.open = (value) => {
        open(value);
        return null;
    };
    runtimeWindow.open = patchedOpen;

    const handleClick = (event: MouseEvent) => {
        const anchor = newContextAnchor(event);
        const href = anchor?.getAttribute("href");
        if (!href) {
            return;
        }

        event.preventDefault();
        open(href);
    };
    runtimeDocument.addEventListener("click", handleClick, true);

    return () => {
        runtimeDocument.removeEventListener("click", handleClick, true);
        if (runtimeWindow.open === patchedOpen) {
            runtimeWindow.open = originalOpen;
        }
    };
}
