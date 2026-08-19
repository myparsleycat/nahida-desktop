import path from "node:path";

import { extractMergedModPaths } from "../services/mod-manager/merge/ini-text";

export function hasMergedModHeader(text: string) {
    return extractMergedModPaths(text).length > 0;
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
