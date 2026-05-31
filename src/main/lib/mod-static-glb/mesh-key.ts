import { trimResourcePrefix } from "./override-analysis";
import { normalizeKey } from "./shared";

export function canonicalizeMeshFamilyKey(value: string): string {
    const numericSuffix = extractNumericSuffix(value);
    const withSuffixRemoved = value.replace(/(?:\.\d+)?$/i, "");
    const withoutResourcePrefix = trimResourcePrefix(withSuffixRemoved);
    const withoutIbSuffix = withoutResourcePrefix.replace(/(?:indexbuffer|ib)$/i, "");
    const normalizedFamily = withoutIbSuffix.replace(
        /(head|body|dress|hair|face|weapon|cloth|skirt|shoe|arm|leg|hand|foot)(?:[a-z0-9]+)?$/i,
        "$1",
    );
    return `${normalizedFamily}${numericSuffix !== null ? `.${suffixToString(numericSuffix)}` : ""}`;
}

export function extractNumericSuffix(value: string): number | null {
    const match = value.match(/\.([0-9]+)$/);
    return match ? Number(match[1]) : null;
}

function suffixToString(value: number): string {
    return String(value);
}

function stripNormalizedSuffix(value: string): string {
    return value.replace(/\d+$/, "");
}

function baseMatches(stem: string, name: string, key: string): boolean {
    const keyBase = stripNormalizedSuffix(key);
    const stemBase = stripNormalizedSuffix(stem);
    const nameBase = stripNormalizedSuffix(name);
    return (
        stemBase.includes(keyBase) ||
        nameBase.includes(keyBase) ||
        keyBase.includes(stemBase) ||
        keyBase.includes(nameBase)
    );
}

export function keyMatchesIb(groupKey: string, ibKey: string): boolean {
    const a = normalizeKey(canonicalizeMeshFamilyKey(groupKey));
    const b = normalizeKey(canonicalizeMeshFamilyKey(ibKey));
    if (a === b) {
        return true;
    }

    const groupSuffix = extractNumericSuffix(groupKey);
    const ibSuffix = extractNumericSuffix(ibKey);
    if (groupSuffix !== null || ibSuffix !== null) {
        if (groupSuffix !== null && ibSuffix !== null && groupSuffix !== ibSuffix) {
            return false;
        }

        const groupBase = normalizeKey(canonicalizeMeshFamilyKey(groupKey).replace(/\.\d+$/i, ""));
        const ibBase = normalizeKey(canonicalizeMeshFamilyKey(ibKey).replace(/\.\d+$/i, ""));
        return groupBase === ibBase || groupBase.includes(ibBase) || ibBase.includes(groupBase);
    }

    return a.includes(b) || b.includes(a);
}

export function strictKeyMatchesIb(groupKey: string, ibKey: string): boolean {
    const a = normalizeKey(canonicalizeMeshFamilyKey(groupKey));
    const b = normalizeKey(canonicalizeMeshFamilyKey(ibKey));
    if (a === b) {
        return true;
    }

    const groupSuffix = extractNumericSuffix(groupKey);
    const ibSuffix = extractNumericSuffix(ibKey);
    if (groupSuffix !== null || ibSuffix !== null) {
        if (groupSuffix !== ibSuffix) {
            return false;
        }

        const groupBase = normalizeKey(canonicalizeMeshFamilyKey(groupKey).replace(/\.\d+$/i, ""));
        const ibBase = normalizeKey(canonicalizeMeshFamilyKey(ibKey).replace(/\.\d+$/i, ""));
        return groupBase === ibBase;
    }

    return false;
}

export function bestKeyForIb(stem: string, resourceName: string, keys: string[]): string {
    const normalizedStem = normalizeKey(canonicalizeMeshFamilyKey(stem));
    const normalizedName = normalizeKey(canonicalizeMeshFamilyKey(resourceName));
    const sorted = [...keys].sort((a, b) => b.length - a.length);
    const suffix = extractNumericSuffix(resourceName) ?? extractNumericSuffix(stem);
    const sameSuffixKeys =
        suffix !== null ? sorted.filter((key) => extractNumericSuffix(key) === suffix) : [];

    const exactKeyMatch =
        sameSuffixKeys.find(
            (key) => normalizeKey(canonicalizeMeshFamilyKey(key)) === normalizedName,
        ) ||
        sameSuffixKeys.find(
            (key) => normalizeKey(canonicalizeMeshFamilyKey(key)) === normalizedStem,
        ) ||
        sorted.find((key) => normalizeKey(canonicalizeMeshFamilyKey(key)) === normalizedName) ||
        sorted.find((key) => normalizeKey(canonicalizeMeshFamilyKey(key)) === normalizedStem);
    if (exactKeyMatch) {
        return exactKeyMatch;
    }

    if (sameSuffixKeys.length === 1) {
        return sameSuffixKeys[0];
    }

    return (
        sameSuffixKeys.find((key) =>
            baseMatches(
                normalizedStem,
                normalizedName,
                normalizeKey(canonicalizeMeshFamilyKey(key)),
            ),
        ) ||
        sorted.find((key) =>
            baseMatches(
                normalizedStem,
                normalizedName,
                normalizeKey(canonicalizeMeshFamilyKey(key)),
            ),
        ) ||
        stem
    );
}
