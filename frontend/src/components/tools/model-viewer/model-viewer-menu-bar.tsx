import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import {
  Menubar,
  MenubarCheckboxItem,
  MenubarContent,
  MenubarGroup,
  MenubarItem,
  MenubarLabel,
  MenubarMenu,
  MenubarRadioGroup,
  MenubarRadioItem,
  MenubarSeparator,
  MenubarTrigger,
} from "@renderer/components/ui/menubar";
import { BrushIcon, CameraIcon, RotateCcwIcon, SaveIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import type {
  ModelViewerThreeEnvironment,
  ModelViewerThreeToneMapping,
} from "./model-viewer-contract";

import {
  DEFAULT_THREE_EXPOSURE,
  MAX_THREE_EXPOSURE,
  MIN_THREE_EXPOSURE,
  MODEL_ROTATION_ACTIONS,
} from "./model-viewer-dialog-types";
import { formatSliderValue } from "./model-viewer-dialog-variants";

export type BrushMode = "paint" | "erase";

export interface BrushProps {
  enabled: boolean;
  onEnabledChange: (enabled: boolean) => void;
  mode: BrushMode;
  onModeChange: (mode: BrushMode) => void;
  radius: number;
  onRadiusChange: (radius: number) => void;
  strength: number;
  onStrengthChange: (strength: number) => void;
  mirrorX: boolean;
  onMirrorXChange: (mirrorX: boolean) => void;
  onResetPaintedWeights?: () => void;
  hasPaintedWeights?: boolean;
}

export interface ModelViewerMenuBarProps {
  /** Rotates the model by [roll, pitch, yaw] delta in degrees */
  rotateModel: (delta: [number, number, number]) => void;
  /** Resets the viewport to the initial camera state */
  onResetView: () => void;

  /* Texture */
  doubleSidedEnabled: boolean;
  onDoubleSidedChange: (value: boolean) => void;

  /* Rendering */
  toneMapping: ModelViewerThreeToneMapping;
  onToneMappingChange: (value: ModelViewerThreeToneMapping) => void;
  environment: ModelViewerThreeEnvironment;
  onEnvironmentChange: (value: ModelViewerThreeEnvironment) => void;
  exposure: number;
  onExposureDraftChange: (value: number) => void;
  onExposureCommit: (value: number) => void;

  /* Toggle (variant mode only) */
  showToggleViewer: boolean;
  isViewerBusy: boolean;
  onSaveTogglesToIni: () => void;
  onResetToggles: () => void;

  /* Preview capture */
  canSaveCapturedPreview: boolean;
  onCapturePreviewClick: () => void;

  /* Brush (Body shape weight painting) */
  brushProps?: BrushProps;

  /** Hide texture menu when unused by the host. */
  showTextureMenu?: boolean;
  /** Hide rendering menu when unused by the host. */
  showRenderingMenu?: boolean;
  /** Hide misc menu (e.g. capture preview) when unused by the host. */
  showMiscMenu?: boolean;
}

export function ModelViewerMenuBar({
  rotateModel,
  onResetView,
  doubleSidedEnabled,
  onDoubleSidedChange,
  toneMapping,
  onToneMappingChange,
  environment,
  onEnvironmentChange,
  exposure,
  onExposureDraftChange,
  onExposureCommit,
  showToggleViewer,
  isViewerBusy,
  onSaveTogglesToIni,
  onResetToggles,
  canSaveCapturedPreview,
  onCapturePreviewClick,
  brushProps,
  showTextureMenu = true,
  showRenderingMenu = true,
  showMiscMenu = true,
}: ModelViewerMenuBarProps) {
  const { t } = useTranslation();

  return (
    <Menubar>
      <MenubarMenu>
        <MenubarTrigger>{t("page.tools.model_viewer.menu.model")}</MenubarTrigger>
        <MenubarContent>
          <MenubarGroup>
            <MenubarLabel className="text-xs text-muted-foreground">
              {t("page.tools.model_viewer.menu.rotate")}
            </MenubarLabel>
            {MODEL_ROTATION_ACTIONS.map((action) => (
              <MenubarItem key={action.label} onClick={() => rotateModel(action.delta)}>
                {t(`page.tools.model_viewer.rotate_actions.${action.label}`)}
              </MenubarItem>
            ))}
          </MenubarGroup>
          <MenubarSeparator />
          <MenubarGroup>
            <MenubarItem onClick={onResetView}>
              <RotateCcwIcon />
              {t("page.tools.model_viewer.menu.reset")}
            </MenubarItem>
          </MenubarGroup>
        </MenubarContent>
      </MenubarMenu>
      {brushProps ? (
        <MenubarMenu>
          <MenubarTrigger className={brushProps.enabled ? "font-semibold text-primary" : undefined}>
            <BrushIcon className="mr-1 h-3.5 w-3.5" />
            {t("page.tools.body_shape.brush_menu")}
          </MenubarTrigger>
          <MenubarContent>
            <MenubarGroup>
              <MenubarCheckboxItem
                checked={brushProps.enabled}
                onCheckedChange={(checked) => brushProps.onEnabledChange(checked === true)}
              >
                {t("page.tools.body_shape.brush_enable")}
              </MenubarCheckboxItem>
            </MenubarGroup>
            <MenubarSeparator />
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">
                {t("page.tools.body_shape.brush_mode")}
              </MenubarLabel>
              <MenubarRadioGroup
                value={brushProps.mode}
                onValueChange={(val) => brushProps.onModeChange(val as BrushMode)}
              >
                <MenubarRadioItem value="paint">
                  {t("page.tools.body_shape.brush_mode_paint")}
                </MenubarRadioItem>
                <MenubarRadioItem value="erase">
                  {t("page.tools.body_shape.brush_mode_erase")}
                </MenubarRadioItem>
              </MenubarRadioGroup>
            </MenubarGroup>
            <MenubarSeparator />
            <MenubarGroup>
              <MenubarCheckboxItem
                checked={brushProps.mirrorX}
                onCheckedChange={(checked) => brushProps.onMirrorXChange(checked === true)}
              >
                {t("page.tools.body_shape.brush_mirror_x")}
              </MenubarCheckboxItem>
            </MenubarGroup>
            <MenubarSeparator />
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">
                {t("page.tools.body_shape.brush_radius")} ({formatSliderValue(brushProps.radius)})
              </MenubarLabel>
              <div className="px-1.5 py-1">
                <Input
                  type="number"
                  min={0.01}
                  max={2.0}
                  step={0.01}
                  value={formatSliderValue(brushProps.radius)}
                  onChange={(event) => {
                    const nextVal = Number.parseFloat(event.target.value);
                    if (Number.isFinite(nextVal) && nextVal > 0) {
                      brushProps.onRadiusChange(nextVal);
                    }
                  }}
                />
              </div>
            </MenubarGroup>
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">
                {t("page.tools.body_shape.brush_strength")} (
                {formatSliderValue(brushProps.strength)})
              </MenubarLabel>
              <div className="px-1.5 py-1">
                <Input
                  type="number"
                  min={0.01}
                  max={1.0}
                  step={0.05}
                  value={formatSliderValue(brushProps.strength)}
                  onChange={(event) => {
                    const nextVal = Number.parseFloat(event.target.value);
                    if (Number.isFinite(nextVal) && nextVal > 0) {
                      brushProps.onStrengthChange(nextVal);
                    }
                  }}
                />
              </div>
            </MenubarGroup>
            {brushProps.onResetPaintedWeights ? (
              <>
                <MenubarSeparator />
                <MenubarGroup>
                  <MenubarItem
                    onClick={brushProps.onResetPaintedWeights}
                    disabled={!brushProps.hasPaintedWeights}
                  >
                    <RotateCcwIcon className="mr-1 h-3.5 w-3.5" />
                    {t("page.tools.body_shape.brush_reset")}
                  </MenubarItem>
                </MenubarGroup>
              </>
            ) : null}
          </MenubarContent>
        </MenubarMenu>
      ) : null}
      {showTextureMenu ? (
        <MenubarMenu>
          <MenubarTrigger>{t("page.tools.model_viewer.menu.texture")}</MenubarTrigger>
          <MenubarContent>
            <MenubarGroup>
              <MenubarCheckboxItem
                checked={doubleSidedEnabled}
                onCheckedChange={(checked) => onDoubleSidedChange(checked === true)}
              >
                Double Sided
              </MenubarCheckboxItem>
            </MenubarGroup>
          </MenubarContent>
        </MenubarMenu>
      ) : null}
      {showRenderingMenu ? (
        <MenubarMenu>
          <MenubarTrigger>{t("page.tools.model_viewer.menu.rendering")}</MenubarTrigger>
          <MenubarContent>
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">Tone Mapping</MenubarLabel>
              <MenubarRadioGroup
                value={toneMapping}
                onValueChange={(value) => onToneMappingChange(value as ModelViewerThreeToneMapping)}
              >
                <MenubarRadioItem value="neutral">Neutral</MenubarRadioItem>
                <MenubarRadioItem value="aces">ACES Filmic</MenubarRadioItem>
                <MenubarRadioItem value="none">None</MenubarRadioItem>
              </MenubarRadioGroup>
            </MenubarGroup>
            <MenubarSeparator />
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">Environment</MenubarLabel>
              <MenubarRadioGroup
                value={environment}
                onValueChange={(value) => onEnvironmentChange(value as ModelViewerThreeEnvironment)}
              >
                <MenubarRadioItem value="studio">Studio</MenubarRadioItem>
                <MenubarRadioItem value="soft">Soft</MenubarRadioItem>
                <MenubarRadioItem value="none">None</MenubarRadioItem>
              </MenubarRadioGroup>
            </MenubarGroup>
            <MenubarSeparator />
            <MenubarGroup>
              <MenubarLabel className="text-xs text-muted-foreground">Exposure</MenubarLabel>
              <div className="px-1.5 py-1">
                <div className="mb-2 flex items-center gap-1">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 px-2"
                    onClick={() => onExposureCommit(exposure - 0.1)}
                  >
                    -0.1
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 px-2"
                    onClick={() => onExposureCommit(DEFAULT_THREE_EXPOSURE)}
                  >
                    Reset
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 px-2"
                    onClick={() => onExposureCommit(exposure + 0.1)}
                  >
                    +0.1
                  </Button>
                </div>
                <Input
                  type="number"
                  min={MIN_THREE_EXPOSURE}
                  max={MAX_THREE_EXPOSURE}
                  step={0.05}
                  value={formatSliderValue(exposure)}
                  onChange={(event) => {
                    const nextValue = Number.parseFloat(event.target.value);
                    if (Number.isFinite(nextValue)) {
                      onExposureDraftChange(nextValue);
                    }
                  }}
                  onBlur={(event) => {
                    onExposureCommit(Number.parseFloat(event.target.value));
                  }}
                />
                <div className="mt-2 flex items-center justify-between text-[11px] text-muted-foreground">
                  <span>{formatSliderValue(MIN_THREE_EXPOSURE)}</span>
                  <span>{formatSliderValue(MAX_THREE_EXPOSURE)}</span>
                </div>
              </div>
            </MenubarGroup>
          </MenubarContent>
        </MenubarMenu>
      ) : null}
      {showToggleViewer ? (
        <MenubarMenu>
          <MenubarTrigger>{t("page.tools.model_viewer.menu.toggle")}</MenubarTrigger>
          <MenubarContent>
            <MenubarGroup>
              <MenubarItem onClick={onSaveTogglesToIni} disabled={isViewerBusy}>
                <SaveIcon />
                {t("page.tools.model_viewer.menu.save_to_ini")}
              </MenubarItem>
              <MenubarSeparator />
              <MenubarItem onClick={onResetToggles}>
                <RotateCcwIcon />
                {t("page.tools.model_viewer.menu.reset")}
              </MenubarItem>
            </MenubarGroup>
          </MenubarContent>
        </MenubarMenu>
      ) : null}
      {showMiscMenu ? (
        <MenubarMenu>
          <MenubarTrigger>{t("page.tools.model_viewer.menu.misc")}</MenubarTrigger>
          <MenubarContent>
            <MenubarGroup>
              <MenubarItem onClick={onCapturePreviewClick} disabled={!canSaveCapturedPreview}>
                <CameraIcon />
                {t("page.tools.model_viewer.menu.capture_set_preview")}
              </MenubarItem>
            </MenubarGroup>
          </MenubarContent>
        </MenubarMenu>
      ) : null}
    </Menubar>
  );
}
