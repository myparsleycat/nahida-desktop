export type StaticGlbTextureFormat = "png" | "jpeg-safe" | "jpeg-force";

export type PreparedTexture = {
    image?: Buffer;
    imageName: string;
    imagePath?: string;
    mimeType: "image/png" | "image/jpeg";
    alphaMode?: "MASK";
    alphaCutoff?: number;
    usesAlpha: boolean;
    invertedAlpha: boolean;
    selectionScore: number;
    srgbConfidence: "srgb" | "linear" | "unknown";
};

export function textureNamePriority(
    resourceName: string,
    normalizeKey: (value: string) => string,
): number {
    const key = normalizeKey(resourceName);
    let score = 0;

    if (key.includes("basecolor") || key.includes("albedo")) score += 80;
    if (key.includes("diffuse")) score += 60;
    if (key.includes("color")) score += 25;
    if (key.includes("shadow")) score -= 20;
    if (key.includes("lightmap")) score -= 12;
    if (key.includes("light")) score -= 10;
    if (key.includes("metal") || key.includes("rough") || key.includes("ao")) score -= 24;
    if (key.includes("mask")) score -= 28;
    if (key.includes("normal") || key.includes("bump")) score -= 60;

    return score;
}
