import { BodyShapeViewport } from "@renderer/components/tools/body-shape/body-shape-viewport";
import { DEFAULT_THREE_EXPOSURE } from "@renderer/components/tools/model-viewer/model-viewer-dialog-types";
import { ModelViewerMenuBar } from "@renderer/components/tools/model-viewer/model-viewer-menu-bar";
import { Button } from "@renderer/components/ui/button";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { FolderOpenIcon, Loader2Icon, Undo2Icon } from "lucide-react";
import { useMemo } from "react";

import { ALL_ZONES } from "./touch-profile-defaults";
import { TouchProfileReview } from "./touch-profile-review";
import { TouchProfileSelection } from "./touch-profile-selection";
import { useTouchProfileControls } from "./use-touch-profile-controls";
import { useTouchProfileSession } from "./use-touch-profile-session";

export default function TouchProfileTool({
  fixedTargetPath,
  modName,
  onApplied,
  onRolledBack,
}: {
  fixedTargetPath?: string;
  modName?: string;
  onApplied?: (result: { outputModRoot: string; sourceModRoot: string }) => void;
  onRolledBack?: (sourceModRoot: string) => void;
} = {}) {
  const session = useTouchProfileSession({
    fixedTargetPath,
    onApplied,
    onRolledBack,
    onResetSelectionPreview: () => controls.resetSelectionPreview(),
  });
  const controls = useTouchProfileControls(session);
  const {
    t,
    modPath,
    setModPath,
    loading,
    applying,
    rollingBack,
    phase,
    result,
    inputError,
    isFixedTarget,
    activeZoneId,
    activePreview,
    displayPreview,
    selectFolder,
    discardDraft,
    openResult,
    rollbackResult,
    meshPreview,
    meshPreviewLoading,
    meshPreviewError,
    previewLoading,
    previewReloadVersion,
  } = session;
  const { viewportRef, modelOrientation, rotateModel, resetView } = controls;
  const visibleZones = useMemo(() => {
    if (!activePreview) return [];
    if (activeZoneId === ALL_ZONES) return activePreview.zones;
    return activePreview.zones.filter((zone) => zone.id === activeZoneId);
  }, [activePreview, activeZoneId]);
  const previewRegions = useMemo(
    () =>
      visibleZones.map((zone) => ({
        id: zone.id,
        weights: zone.weights,
        amount: 1,
        axisScale: [1, 1, 1] as [number, number, number],
        pivot: zone.center,
      })),
    [visibleZones],
  );
  const emptyMessage = loading
    ? t("page.tools.touch_profile.analyzing")
    : previewLoading
      ? t("page.tools.touch_profile.preview_loading")
      : t("page.tools.touch_profile.preview_empty");
  const meshPreviewRegions = useMemo(
    () => [
      {
        id: "__mesh__",
        weights: new Float32Array(Math.floor((meshPreview?.positions.length ?? 0) / 3)),
        amount: 1,
        axisScale: [1, 1, 1] as [number, number, number],
        pivot: [0, 0, 0] as [number, number, number],
      },
    ],
    [meshPreview],
  );
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3">
      {(phase === "select" && meshPreview) || (phase === "review" && activePreview) ? (
        <ModelViewerMenuBar
          rotateModel={rotateModel}
          onResetView={resetView}
          doubleSidedEnabled={true}
          onDoubleSidedChange={() => {}}
          toneMapping="neutral"
          onToneMappingChange={() => {}}
          environment="studio"
          onEnvironmentChange={() => {}}
          exposure={DEFAULT_THREE_EXPOSURE}
          onExposureDraftChange={() => {}}
          onExposureCommit={() => {}}
          showToggleViewer={false}
          isViewerBusy={previewLoading || loading || meshPreviewLoading}
          onSaveTogglesToIni={() => {}}
          onResetToggles={() => {}}
          canSaveCapturedPreview={false}
          onCapturePreviewClick={() => {}}
          showTextureMenu={false}
          showRenderingMenu={false}
          showMiscMenu={false}
        />
      ) : null}

      <div className="grid min-h-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="relative min-h-80 overflow-hidden rounded-md border bg-muted/30">
          {phase === "select" && meshPreview ? (
            <BodyShapeViewport
              ref={viewportRef}
              originalPositions={meshPreview.positions}
              previewPositions={meshPreview.positions}
              regions={meshPreviewRegions}
              indices={meshPreview.indices}
              showOriginal={false}
              showWeights
              weightVersion={0}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={meshPreview.sessionId}
            />
          ) : displayPreview ? (
            <BodyShapeViewport
              ref={viewportRef}
              originalPositions={displayPreview.positions}
              previewPositions={displayPreview.positions}
              regions={previewRegions}
              indices={displayPreview.indices}
              showOriginal={false}
              showWeights={true}
              weightVersion={previewReloadVersion}
              positionsChanged={false}
              orientation={modelOrientation}
              frameKey={displayPreview.sessionId}
            />
          ) : (
            <div className="flex h-full items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {emptyMessage}
            </div>
          )}
          {loading || previewLoading || meshPreviewLoading ? (
            <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/20">
              <div className="inline-flex items-center gap-2 rounded-md border bg-background/90 px-3 py-2 text-sm shadow-sm">
                <Loader2Icon className="size-4 animate-spin" />
                {loading
                  ? t("page.tools.touch_profile.analyzing")
                  : meshPreviewLoading
                    ? t("page.tools.touch_profile.preview_loading")
                    : t("page.tools.touch_profile.preview_loading")}
              </div>
            </div>
          ) : null}
          {phase === "select" && meshPreviewError ? (
            <div className="absolute inset-x-0 bottom-0 p-2 text-center text-xs text-destructive">
              {meshPreviewError}
            </div>
          ) : null}
        </div>

        <div className="flex min-h-0 flex-col overflow-hidden rounded-md border bg-card/20">
          <div className="border-b px-4 py-3">
            <div className="text-sm font-medium">
              {t("page.tools.touch_profile.title")} ({t("g.beta")})
            </div>
            {modName ? (
              <div className="truncate text-xs text-muted-foreground">{modName}</div>
            ) : null}
          </div>

          <ScrollArea className="min-h-0 flex-1">
            <div className="flex flex-col gap-4 p-4">
              {!isFixedTarget ? (
                <Field>
                  <FieldLabel>{t("page.tools.touch_profile.mod_path")}</FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      value={modPath}
                      onChange={(event) => setModPath(event.target.value)}
                      placeholder={t("page.tools.touch_profile.mod_path_placeholder")}
                    />
                    <Button type="button" variant="outline" size="icon" onClick={selectFolder}>
                      <FolderOpenIcon className="size-4" />
                    </Button>
                  </div>
                </Field>
              ) : null}

              {phase === "select" ? null : (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void discardDraft()}
                  disabled={Boolean(result) || applying}
                >
                  {t("page.tools.touch_profile.discard")}
                </Button>
              )}

              {inputError ? (
                <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                  <div className="font-medium">
                    {t(`page.tools.touch_profile.input_error.${inputError}.title`)}
                  </div>
                  <div className="mt-1">
                    {t(`page.tools.touch_profile.input_error.${inputError}.description`)}
                  </div>
                </div>
              ) : null}

              {<TouchProfileSelection session={session} controls={controls} />}

              {<TouchProfileReview session={session} />}

              {result ? (
                <div className="rounded-md border border-border px-3 py-2 text-xs">
                  <div className="font-medium">{t("page.tools.touch_profile.result")}</div>
                  <div className="mt-1 break-all text-muted-foreground">{result.outputModRoot}</div>
                  <div className="mt-2 text-muted-foreground">
                    {t("page.tools.touch_profile.source")}:{" "}
                    <span className="break-all">{result.sourceModRoot}</span>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => void openResult()}
                      disabled={rollingBack}
                    >
                      {t("page.tools.touch_profile.open_folder")}
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() => void rollbackResult()}
                      disabled={rollingBack}
                    >
                      {rollingBack ? (
                        <Loader2Icon className="mr-2 size-4 animate-spin" />
                      ) : (
                        <Undo2Icon className="mr-2 size-4" />
                      )}
                      {rollingBack
                        ? t("page.tools.touch_profile.rolling_back")
                        : t("page.tools.touch_profile.rollback")}
                    </Button>
                  </div>
                </div>
              ) : null}
            </div>
          </ScrollArea>
        </div>
      </div>
    </div>
  );
}
