import type {
    ClassifyMergePacksResult,
    MergeEngine,
    MergePackClassification,
    MergePlanGroup,
    MergePlanNode,
} from "@shared/types";

export const THREEDMIGOTO_IMPORTERS = new Set(["GIMI", "SRMI", "ZZMI", "HIMI", "WWMI", "EFMI"]);

export function isThreedmigotoImporter(importer: string | null | undefined) {
    return THREEDMIGOTO_IMPORTERS.has((importer ?? "").toUpperCase());
}

export function canUseClassic(pack: MergePackClassification) {
    return pack.allowsClassic;
}

export function suggestedEngine(packs: MergePackClassification[]): MergeEngine {
    if (packs.some((pack) => !canUseClassic(pack))) return "namespace";
    return "classic";
}

export function buildDefaultPlan(
    result: ClassifyMergePacksResult,
    packName: string,
): MergePlanGroup | null {
    const usable = result.packs.filter((pack) => pack.family !== "support");
    if (usable.length === 0) return null;
    const engine = suggestedEngine(usable);
    return {
        kind: "group",
        id: "root",
        engine,
        name: packName.replace(/\s+/g, "") || "Merged",
        forwardKey: "vk_right",
        backKey: "vk_left",
        includeVanilla: engine === "namespace",
        children: usable.map((pack) => ({ kind: "leaf", path: pack.path })),
    };
}

export function collectLeaves(node: MergePlanNode): string[] {
    if (node.kind === "leaf") return [node.path];
    return node.children.flatMap(collectLeaves);
}

export function planHasClassicViolation(
    node: MergePlanNode,
    packsByPath: Map<string, MergePackClassification>,
): boolean {
    if (node.kind === "leaf") return false;
    if (node.engine === "classic") {
        const blocked = collectLeaves(node).some((leafPath) => {
            const pack = packsByPath.get(leafPath);
            return !pack || !canUseClassic(pack);
        });
        if (blocked) return true;
    }
    return node.children.some((child) => planHasClassicViolation(child, packsByPath));
}

export function planIsValid(node: MergePlanNode | null | undefined): node is MergePlanNode {
    if (!node) return false;
    if (node.kind === "leaf") return true;
    if (!node.name.trim() || !node.forwardKey.trim()) return false;
    if (node.engine === "namespace" && !node.backKey.trim()) return false;
    const leaves = collectLeaves(node);
    return leaves.length >= 2 && node.children.every(planIsValid);
}
