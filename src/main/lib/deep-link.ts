export function getNahidaDeepLinkRoute(commandLine: string[]) {
    return commandLine.map(parseNahidaDeepLink).find((route) => route !== null) ?? null;
}

export function parseNahidaDeepLink(value: string) {
    try {
        const url = new URL(value);
        if (url.protocol !== "nahida:" || url.hostname.toLowerCase() !== "gamebanana") return null;

        const pathMatch = /^\/(?:mods?|open)\/(\d+)\/?$/i.exec(url.pathname);
        const modId = pathMatch?.[1] ?? url.searchParams.get("id");
        if (modId && isValidModId(modId)) return `/gamebanana?mod=${modId}`;

        const sourceUrl = url.searchParams.get("url");
        if (!sourceUrl) return null;

        const gameBananaUrl = new URL(sourceUrl);
        if (
            !["http:", "https:"].includes(gameBananaUrl.protocol) ||
            !["gamebanana.com", "www.gamebanana.com"].includes(gameBananaUrl.hostname.toLowerCase())
        ) {
            return null;
        }

        const sourceMatch = /^\/mods\/(\d+)\/?$/i.exec(gameBananaUrl.pathname);
        if (!sourceMatch || !isValidModId(sourceMatch[1])) return null;
        return `/gamebanana?mod=${sourceMatch[1]}`;
    } catch {
        return null;
    }
}

function isValidModId(value: string) {
    const id = Number(value);
    return Number.isSafeInteger(id) && id > 0;
}
