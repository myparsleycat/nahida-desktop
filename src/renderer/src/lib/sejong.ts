import { disassemble } from "es-hangul";

const similarSoundMap = new Map([
    ["ㄱ", ["ㄲ", "ㅋ"]],
    ["ㄷ", ["ㄸ", "ㅌ"]],
    ["ㅂ", ["ㅃ", "ㅍ"]],
    ["ㅅ", ["ㅆ"]],
    ["ㅈ", ["ㅉ", "ㅊ"]],
    ["ㅐ", ["ㅔ"]],
    ["ㅔ", ["ㅐ"]],
]);

const getSimilarPatterns = (str: string): string[] => {
    const patterns: string[] = [str];

    for (let i = 0; i < str.length; i++) {
        const char = str[i];
        const similarChars = similarSoundMap.get(char);

        if (similarChars) {
            similarChars.forEach((similarChar) => {
                patterns.push(str.slice(0, i) + similarChar + str.slice(i + 1));
            });
        }
    }

    return patterns;
};

const isPartialMatch = (target: string, query: string): boolean => {
    const targetJamo = disassemble(target.toLowerCase());
    const queryJamo = disassemble(query.toLowerCase());

    return targetJamo.includes(queryJamo);
};

const isChosungMatch = (chosung: string, query: string): boolean => {
    const disassembledChosung = disassemble(chosung);
    const disassembledQuery = disassemble(query.toLowerCase());
    return disassembledChosung.includes(disassembledQuery);
};

export const getSearchScore = (
    itemName: string,
    query: string,
    cachedData: { lowerName: string; jamo: string; chosung: string },
): number => {
    const { lowerName, jamo, chosung } = cachedData;
    const lowerQuery = query.toLowerCase();

    if (lowerName === lowerQuery) return 100;
    if (lowerName.startsWith(lowerQuery)) return 90;
    if (lowerName.includes(lowerQuery)) return 80;

    if (isChosungMatch(chosung, lowerQuery)) return 75;

    const queryJamo = disassemble(lowerQuery);
    if (isPartialMatch(jamo, queryJamo)) return 70;

    const similarPatterns = getSimilarPatterns(lowerQuery);
    if (similarPatterns.some((pattern) => lowerName.includes(pattern))) return 60;

    return 0;
};
