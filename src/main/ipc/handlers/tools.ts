import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

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
}
