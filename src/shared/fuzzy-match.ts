export type MatchResult = {
    value: string;
    score: number;
};

type RankedMatch = MatchResult & {
    index: number;
    normalizedLength: number;
};

const EXACT_FULL_MATCH_SCORE = 1;
const EXACT_TOKEN_MATCH_SCORE = 0.99;
const INPUT_STARTS_WITH_CANDIDATE_SCORE = 0.97;
const INPUT_INCLUDES_CANDIDATE_SCORE = 0.92;
const TOKEN_LEVENSHTEIN_MAX_SCORE = 0.9;
const FULL_LEVENSHTEIN_MAX_SCORE = 0.8;

export function rankFuzzyMatches(candidates: string[], input: string): MatchResult[] {
    const normalizedInput = normalizeFuzzyString(input);
    const inputTokens = tokenizeFuzzyInput(input);

    return candidates
        .map((candidate, index): RankedMatch => {
            const normalizedCandidate = normalizeFuzzyString(candidate);
            return {
                value: candidate,
                score: scoreCandidate(normalizedCandidate, normalizedInput, inputTokens),
                index,
                normalizedLength: normalizedCandidate.length,
            };
        })
        .sort((a, b) => {
            if (b.score !== a.score) return b.score - a.score;
            if (a.normalizedLength !== b.normalizedLength) {
                return a.normalizedLength - b.normalizedLength;
            }
            return a.index - b.index;
        })
        .map((match) => ({
            value: match.value,
            score: match.score,
        }));
}

export function findBestFuzzyMatch(
    candidates: string[],
    input: string,
    options?: {
        threshold?: number;
    },
): MatchResult | null {
    const [best] = rankFuzzyMatches(candidates, input);
    if (!best) return null;
    if (options?.threshold !== undefined && best.score < options.threshold) return null;
    return best;
}

function normalizeFuzzyString(value: string) {
    return value
        .trim()
        .toLowerCase()
        .normalize("NFKD")
        .replace(/[^\p{L}\p{N}]+/gu, "");
}

function tokenizeFuzzyInput(value: string) {
    return value
        .trim()
        .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
        .split(/[^\p{L}\p{N}]+/u)
        .map(normalizeFuzzyString)
        .filter(Boolean);
}

function scoreCandidate(candidate: string, input: string, inputTokens: string[]) {
    if (!candidate || !input) return 0;
    if (candidate === input) return EXACT_FULL_MATCH_SCORE;
    if (inputTokens.some((token) => token === candidate)) return EXACT_TOKEN_MATCH_SCORE;
    if (input.startsWith(candidate)) return INPUT_STARTS_WITH_CANDIDATE_SCORE;
    if (input.includes(candidate)) return INPUT_INCLUDES_CANDIDATE_SCORE;

    return clampScore(
        Math.max(
            ...inputTokens.map((token) => scoreToken(candidate, token)),
            levenshteinSimilarity(candidate, input) * FULL_LEVENSHTEIN_MAX_SCORE,
        ),
    );
}

function scoreToken(candidate: string, token: string) {
    if (!token) return 0;

    const shorterLength = Math.min(candidate.length, token.length);
    const longerLength = Math.max(candidate.length, token.length);
    const commonPrefixLength = getCommonPrefixLength(candidate, token);
    const tokenDistance = hasSingleAdjacentTransposition(candidate, token)
        ? 1
        : levenshteinDistance(candidate, token);
    const strongPrefixScore =
        commonPrefixLength >= Math.max(2, Math.ceil(shorterLength * 0.6))
            ? 0.85 + 0.1 * (commonPrefixLength / longerLength)
            : 0;
    const shortTokenTypoScore =
        shorterLength >= 4 && tokenDistance === 1 && Math.abs(candidate.length - token.length) <= 1
            ? 0.88
            : 0;

    return Math.max(
        strongPrefixScore,
        shortTokenTypoScore,
        (1 - tokenDistance / longerLength) * TOKEN_LEVENSHTEIN_MAX_SCORE,
    );
}

function getCommonPrefixLength(a: string, b: string) {
    const maxLength = Math.min(a.length, b.length);
    const mismatchIndex = Array.from({ length: maxLength }).findIndex(
        (_, index) => a[index] !== b[index],
    );
    return mismatchIndex === -1 ? maxLength : mismatchIndex;
}

function levenshteinSimilarity(a: string, b: string) {
    if (!a && !b) return 1;
    if (!a || !b) return 0;
    return 1 - levenshteinDistance(a, b) / Math.max(a.length, b.length);
}

function levenshteinDistance(a: string, b: string) {
    const previous = Array.from({ length: b.length + 1 }, (_, index) => index);

    for (let i = 1; i <= a.length; i += 1) {
        const current = [i];

        for (let j = 1; j <= b.length; j += 1) {
            current[j] =
                a[i - 1] === b[j - 1]
                    ? previous[j - 1]
                    : Math.min(previous[j - 1], previous[j], current[j - 1]) + 1;
        }

        previous.splice(0, previous.length, ...current);
    }

    return previous[b.length];
}

function hasSingleAdjacentTransposition(a: string, b: string) {
    if (a.length !== b.length) return false;

    const firstMismatch = Array.from({ length: a.length }).findIndex(
        (_, index) => a[index] !== b[index],
    );
    if (firstMismatch < 0 || firstMismatch + 1 >= a.length) return false;

    return (
        a[firstMismatch] === b[firstMismatch + 1] &&
        a[firstMismatch + 1] === b[firstMismatch] &&
        a.slice(firstMismatch + 2) === b.slice(firstMismatch + 2)
    );
}

function clampScore(score: number) {
    return Math.min(1, Math.max(0, score));
}
