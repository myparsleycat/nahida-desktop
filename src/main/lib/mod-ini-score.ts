import path from "node:path";

const MERGED_MOD_HEADER = /;\s*(?:merged mods?|合并mod)\s*:/i;

export function hasMergedModHeader(text: string) {
    return MERGED_MOD_HEADER.test(text);
}

export function scoreIniCandidate(candidatePath: string, text: string): number {
    const basename = path.basename(candidatePath).toLowerCase();
    let score = 0;

    if (basename === "merged.ini") score += 120;
    if (basename.startsWith("master") && basename.endsWith(".ini")) score += 140;
    if (hasMergedModHeader(text)) score += 80;
    if (/^\s*namespace\s*=.+$/im.test(text)) score += 60;

    const persistCount = (text.match(/^\s*global\s+persist\s+\$/gim) || []).length;
    const cycleCount = (text.match(/^\s*type\s*=\s*cycle\s*$/gim) || []).length;
    const overrideCount = (text.match(/^\s*\[TextureOverride/gim) || []).length;
    const resourceCount = (text.match(/^\s*\[Resource/gim) || []).length;

    score += persistCount * 15;
    score += cycleCount * 10;
    score += Math.min(overrideCount, 50);
    score += Math.min(resourceCount, 50);

    if (/^\s*\[KeyHelp\]/im.test(text)) score -= 25;
    if (basename.startsWith("disabled") && !hasMergedModHeader(text)) score -= 10;

    return score;
}
