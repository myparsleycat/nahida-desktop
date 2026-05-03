import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import type {
    StaticGlbConvertInput,
    StaticGlbViewerInput,
} from "@main/services/mod-tools/static-glb";

export function registerToolsHandlers(d: NahidaDesktop) {
    rh("tools:getTextureResizeSettings", () => d.service.modTools.textureResizer.getSettings());
    rh("tools:listTextureFolder", (targetPath: string, settings) =>
        d.service.modTools.textureResizer.listFolderTextures(targetPath, settings),
    );
    rh("tools:listTextureMod", (modPath: string, settings) =>
        d.service.modTools.textureResizer.listModTextures(modPath, settings),
    );
    rh("tools:resizeTextureFile", (input) => d.service.modTools.textureResizer.resizeFile(input));
    rh("tools:saveTextureResizeSettings", (settings) =>
        d.service.modTools.textureResizer.saveSettings(settings),
    );
    rh("tools:resizeTextureFolder", (input) =>
        d.service.modTools.textureResizer.resizeFolder(input),
    );
    rh("tools:resizeTextureMod", (modPath: string, input) =>
        d.service.modTools.textureResizer.resizeMod(modPath, input),
    );
    rh(
        "tools:buildNewD3DDLL",
        ({
            provider,
            version,
            importerKey,
            importerPath,
        }: {
            provider: string;
            version: string;
            importerKey: string;
            importerPath?: string;
        }) =>
            d.service.modTools.dllBuilder.buildNewD3DDLL({
                provider,
                version,
                importerKey,
                importerPath,
            }),
    );
    rh("tools:getBuilderState", () => d.service.modTools.dllBuilder.getBuilderState());
    rh("tools:getProviderReleases", (provider: string) =>
        d.service.modTools.dllBuilder.getProviderReleases(provider),
    );
    rh("tools:updateReleases", () => d.service.modTools.dllBuilder.updateReleases());
    rh("tools:getStaticGlbAssetPath", () => d.service.modTools.staticGlb.getAssetPath());
    rh("tools:setStaticGlbAssetPath", (assetPath: string) =>
        d.service.modTools.staticGlb.setAssetPath(assetPath),
    );
    rh("tools:getStaticGlbTextureSettings", () =>
        d.service.modTools.staticGlb.getTextureSettings(),
    );
    rh("tools:setStaticGlbTextureFormat", (textureFormat: StaticGlbConvertInput["textureFormat"]) =>
        d.service.modTools.staticGlb.setTextureFormat(textureFormat ?? "jpeg-safe"),
    );
    rh("tools:setStaticGlbJpegQuality", (jpegQuality: number) =>
        d.service.modTools.staticGlb.setJpegQuality(jpegQuality),
    );
    rh("tools:convertStaticGlb", (input: StaticGlbConvertInput) =>
        d.service.modTools.staticGlb.convert(input),
    );
    rh("tools:convertStaticGlbForViewer", (input: StaticGlbViewerInput) =>
        d.service.modTools.staticGlb.convertForViewer(input),
    );
    rh(
        "tools:persistModelViewerToggleState",
        (iniPath: string, state: Record<string, string | number>) =>
            d.service.modTools.togglePersist.persistStateToIni(iniPath, state),
    );
    rh("tools:cleanupStaticGlbViewerFile", (glbPath: string) =>
        d.service.modTools.staticGlb.cleanupViewerFile(glbPath),
    );
}
