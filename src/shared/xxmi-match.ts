export const GAME_MATCH_CASES: Record<string, string[]> = {
    GIMI: ["원신", "genshin", "gimi"],
    SRMI: ["스타레일", "붕스", "열차", "starrail", "srmi"],
    ZZMI: ["젠레스", "젠존제", "찢", "zzz", "zenless", "zzmi"],
    WWMI: ["명조", "묑조", "wuwa", "wuthering", "wwmi", "ww"],
    EFMI: ["엔드필드", "엔필", "endfield", "efmi"],
    HIMI: ["붕괴", "붕괴3", "붕괴3rd", "himi", "honkai", "hi3rd"],
};

export const getMatchingImporter = (
    gameName: string,
    enabledImporters: string[] | null,
): string | null => {
    if (!gameName || !enabledImporters || enabledImporters.length === 0) return null;
    const lowerName = gameName.toLowerCase();

    for (const [importer, keywords] of Object.entries(GAME_MATCH_CASES)) {
        if (!enabledImporters.includes(importer)) {
            continue;
        }

        if (keywords.some((k) => lowerName.includes(k))) {
            return importer;
        }
    }

    return null;
};
