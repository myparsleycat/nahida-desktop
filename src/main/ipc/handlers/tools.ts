import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import type { StaticGlbConvertInput } from "@main/services/mod-tools/static-glb";

export function registerToolsHandlers(d: NahidaDesktop) {
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
    rh("tools:convertStaticGlb", (input: StaticGlbConvertInput) =>
        d.service.modTools.staticGlb.convert(input),
    );
    rh("tools:convertStaticGlbForViewer", (modPath: string) =>
        d.service.modTools.staticGlb.convertForViewer(modPath),
    );
    rh("tools:cleanupStaticGlbViewerFile", (glbPath: string) =>
        d.service.modTools.staticGlb.cleanupViewerFile(glbPath),
    );
}
