import path from "node:path";

import { DISABLED_PREFIX_REGEX, stripDisabledPrefix } from "@shared/mod";
import type {
    ClassifyMergePacksResult,
    MergeDialect,
    MergePackClassification,
} from "@shared/types";
import fg from "fast-glob";
import fse from "fs-extra";

import { scoreIniCandidate } from "../../../lib/mod-ini-score";
import {
    detectDialect,
    extractHashes,
    extractMergedModPaths,
    extractNamespace,
    extractObjectGuid,
    hasCommandListDispatch,
    hasKeySection,
    hasMasterSwapRef,
    hasNumberedResources,
    isBackupIniName,
    isFileDisabledIniName,
    isSupportIniName,
    persistVarCount,
} from "./ini-text";

const SUPPORT_MARKERS = [
    /CommandList\\global\\ORFix/i,
    /CommandList\\TexFx/i,
    /Resource\\ZZMI\\/i,
    /Resource\\SRMI\\/i,
    /Resource\\WWMIv1\\/i,
    /Resource\\EFMIv1\\/i,
    /ShaderFixes\\help/i,
];

export async function classifyMergePacks(modPaths: string[]): Promise<ClassifyMergePacksResult> {
    const packs = await Promise.all(modPaths.map((modPath) => classifyPack(modPath)));
    const comparable = packs.filter((pack) => pack.family !== "support" && pack.hashes.length > 0);
    const overlap =
        comparable.length < 2
            ? true
            : comparable.slice(1).every((pack) => hasOverlap(comparable[0], pack));

    const warnings = [
        ...new Set(packs.flatMap((pack) => pack.warnings)),
        ...(overlap ? [] : ["hash_mismatch"]),
    ];

    return { packs, hashOverlap: overlap, warnings };
}

export async function classifyPack(modPath: string): Promise<MergePackClassification> {
    const name = stripDisabledPrefix(path.basename(modPath));
    const candidates = await collectEnabledInis(modPath);

    if (candidates.length === 0) {
        return {
            path: modPath,
            name,
            family: "support",
            dialect: "unknown",
            primaryIniPath: null,
            hashes: [],
            objectGuid: null,
            allowsClassic: false,
            warnings: ["no_enabled_ini"],
        };
    }

    const scored = await Promise.all(
        candidates.map(async (candidate) => ({
            path: candidate,
            text: await fse.readFile(candidate, "utf8"),
        })),
    );
    scored.sort(
        (left, right) =>
            scoreIniCandidate(right.path, right.text) - scoreIniCandidate(left.path, left.text) ||
            left.path.localeCompare(right.path),
    );

    const primary = scored[0];
    const dialect = detectDialect(primary.text);
    const family = detectFamily(
        primary.path,
        primary.text,
        scored.map((entry) => entry.text).join("\n"),
    );
    const warnings = collectWarnings(family, dialect, primary.text);
    const allowsClassic =
        family === "ordinary" && dialect !== "wwmi" && dialect !== "efmi" && dialect !== "unknown";

    return {
        path: modPath,
        name,
        family,
        dialect,
        primaryIniPath: primary.path,
        hashes: extractHashes(primary.text),
        objectGuid: extractObjectGuid(primary.text),
        allowsClassic,
        warnings,
    };
}

export async function collectEnabledInis(modPath: string) {
    const candidates = await fg("**/*.ini", {
        cwd: modPath,
        absolute: true,
        onlyFiles: true,
        caseSensitiveMatch: false,
    });

    return candidates.filter((candidate) => {
        const fileName = path.basename(candidate);
        if (
            isFileDisabledIniName(fileName) ||
            isBackupIniName(fileName) ||
            isSupportIniName(fileName)
        ) {
            return false;
        }
        return !path
            .relative(modPath, candidate)
            .split(/[\\/]/)
            .slice(0, -1)
            .some((segment) => DISABLED_PREFIX_REGEX.test(segment));
    });
}

function detectFamily(primaryPath: string, primaryText: string, packText: string) {
    const basename = path.basename(primaryPath).toLowerCase();
    const namespace = extractNamespace(primaryText);
    const isMasterName = basename.startsWith("master") && basename.endsWith(".ini");
    const isNamespaceMaster =
        isMasterName ||
        (namespace?.toLowerCase().endsWith("\\master") === true &&
            !/^\s*\[Resource/im.test(primaryText));

    if (isNamespaceMaster || hasMasterSwapRef(packText)) return "namespace_merge" as const;

    if (
        extractMergedModPaths(primaryText).length > 0 ||
        basename === "merged.ini" ||
        (hasNumberedResources(primaryText) && hasCommandListDispatch(primaryText))
    ) {
        return "classic_merge" as const;
    }

    if (isSupportIni(primaryText, basename)) return "support" as const;
    if (hasKeySection(primaryText) || persistVarCount(primaryText) >= 2)
        return "in_mod_toggle" as const;
    return "ordinary" as const;
}

function isSupportIni(text: string, basename: string) {
    if (isSupportIniName(basename)) return true;
    return (
        SUPPORT_MARKERS.some((marker) => marker.test(text)) && !/^\s*\[TextureOverride/im.test(text)
    );
}

function collectWarnings(
    family: MergePackClassification["family"],
    dialect: MergeDialect,
    text: string,
) {
    const warnings: string[] = [];
    if (family === "in_mod_toggle") warnings.push("in_mod_toggle");
    if (family === "support") warnings.push("support");
    if (dialect === "wwmi" || dialect === "efmi") warnings.push("namespace_only_dialect");
    if (dialect === "unknown") warnings.push("unknown_dialect");
    if (extractNamespace(text) && family === "in_mod_toggle") warnings.push("namespaced_toggle");
    return warnings;
}

function hasOverlap(left: MergePackClassification, right: MergePackClassification) {
    if (left.objectGuid && right.objectGuid) return left.objectGuid === right.objectGuid;
    const rightHashes = new Set(right.hashes);
    return left.hashes.some((hash) => rightHashes.has(hash));
}
