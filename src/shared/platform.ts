export function supportsWindowsDesktopFeatures(platform: NodeJS.Platform | null | undefined) {
    return platform === "win32";
}
