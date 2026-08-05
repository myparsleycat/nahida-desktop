import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import type { BodyShapeExportInput } from "@main/services/mod-tools/body-shape";
import type {
    StaticGlbConvertInput,
    StaticGlbViewerInput,
} from "@main/services/mod-tools/static-glb";
import type {
    TouchProfileApplyInput,
    TouchProfileLoadInput,
    TouchProfileRegenerateInput,
    TouchProfileRollbackInput,
    // vision-llm disabled — LLM/vision-only input types isolated
    // TouchProfileLlmApiKeyInput,
    TouchProfileUpdateZoneSettingsInput,
    // TouchProfileReanalyzeTurnInput,
    // TouchProfileSelectTurnInput,
} from "@main/services/mod-tools/touch-profile";
import type { TouchDraft } from "@main/services/mod-tools/touch-profile-types";
import type {
    TouchProfileAnalyzeComponentsInput,
    TouchProfilePreviewInput,
} from "@shared/touch-profile-preview";

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
        "tools:4001FixerBuildDll",
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
            d.service.modTools.fourThousandOneFixer.buildD3D11Dll({
                provider,
                version,
                importerKey,
                importerPath,
            }),
    );
    rh(
        "tools:4001FixerDiversifyDllPadding",
        ({ importerKey, importerPath }: { importerKey: string; importerPath?: string }) =>
            d.service.modTools.fourThousandOneFixer.diversifyD3D11DllPadding({
                importerKey,
                importerPath,
            }),
    );
    rh("tools:4001FixerRestoreDiversifiedDll", ({ importerPath }: { importerPath?: string }) =>
        d.service.modTools.fourThousandOneFixer.restoreDiversifiedD3D11Dll({
            importerPath,
        }),
    );
    rh("tools:4001FixerGetState", () => d.service.modTools.fourThousandOneFixer.getState());
    rh("tools:4001FixerGetDiversificationState", ({ importerPath }: { importerPath?: string }) =>
        d.service.modTools.fourThousandOneFixer.getDiversificationState({ importerPath }),
    );
    rh("tools:4001FixerCheckImporterWriteAccess", ({ importerPath }: { importerPath?: string }) =>
        d.service.modTools.fourThousandOneFixer.checkImporterWriteAccess({ importerPath }),
    );
    rh("tools:4001FixerGetProviderReleases", (provider: string) =>
        d.service.modTools.fourThousandOneFixer.getProviderReleases(provider),
    );
    rh("tools:4001FixerUpdateReleases", () =>
        d.service.modTools.fourThousandOneFixer.updateReleases(),
    );
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
    rh("tools:cleanupStaticGlbViewerFile", (glbPath: string, memorySessionId?: string) =>
        d.service.modTools.staticGlb.cleanupViewerFile(glbPath, memorySessionId),
    );
    rh("tools:bodyShapeLoadMod", (modPath: string) =>
        d.service.modTools.bodyShape.loadMod(modPath),
    );
    rh("tools:bodyShapeExport", (input: BodyShapeExportInput) =>
        d.service.modTools.bodyShape.exportMesh(input),
    );
    // vision-llm disabled — loadMod isolated
    // rh("tools:touchProfileLoadMod", (input: TouchProfileLoadInput | string) =>
    //     d.service.modTools.touchProfile.loadMod(input),
    // );
    rh("tools:touchProfilePrepare", (input: TouchProfileLoadInput | string) =>
        d.service.modTools.touchProfile.prepareMod(input),
    );
    rh("tools:touchProfileGetMeshPreview", (input: TouchProfilePreviewInput) =>
        d.service.modTools.touchProfile.getMeshPreview(input),
    );
    rh("tools:touchProfileAnalyzeComponents", (input: TouchProfileAnalyzeComponentsInput) =>
        d.service.modTools.touchProfile.analyzeComponents(input),
    );
    rh("tools:touchProfileGetPreview", (input: TouchProfilePreviewInput) =>
        d.service.modTools.touchProfile.getPreview(input),
    );
    rh("tools:touchProfileSaveDraft", (draft: TouchDraft) =>
        d.service.modTools.touchProfile.saveDraft(draft),
    );
    // vision-llm disabled — vision cache / LLM settings handlers isolated
    // rh("tools:touchProfileClearVisionCache", () =>
    //     d.service.modTools.touchProfile.clearVisionCache(),
    // );
    // rh("tools:touchProfileGetLlmSettings", () => d.service.modTools.touchProfile.getLlmSettings());
    // rh("tools:touchProfileSetLlmApiKey", (input: TouchProfileLlmApiKeyInput) =>
    //     d.service.modTools.touchProfile.setLlmApiKey(input),
    // );
    // rh("tools:touchProfileClearLlmApiKey", () => d.service.modTools.touchProfile.clearLlmApiKey());
    rh("tools:touchProfileUpdateZoneSettings", (input: TouchProfileUpdateZoneSettingsInput) =>
        d.service.modTools.touchProfile.updateZoneSettings(input),
    );
    // vision-llm disabled — reanalyze/selectTurn (vision-only) handlers isolated
    // rh("tools:touchProfileReanalyzeTurn", (input: TouchProfileReanalyzeTurnInput) =>
    //     d.service.modTools.touchProfile.reanalyzeTurn(input),
    // );
    // rh("tools:touchProfileSelectTurn", (input: TouchProfileSelectTurnInput) =>
    //     d.service.modTools.touchProfile.selectTurn(input),
    // );
    rh("tools:touchProfileDiscardDraft", (sessionId: string) =>
        d.service.modTools.touchProfile.discardDraft(sessionId),
    );
    rh("tools:touchProfileApply", (input: TouchProfileApplyInput) =>
        d.service.modTools.touchProfile.apply(input),
    );
    rh("tools:touchProfileRegenerate", (input: TouchProfileRegenerateInput) =>
        d.service.modTools.touchProfile.regenerate(input),
    );
    rh("tools:touchProfileRollback", (input: TouchProfileRollbackInput) =>
        d.service.modTools.touchProfile.rollback(input),
    );
}
