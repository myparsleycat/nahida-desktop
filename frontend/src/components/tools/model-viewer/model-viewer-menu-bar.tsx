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
  MenubarSub,
  MenubarSubContent,
  MenubarSubTrigger,
  MenubarTrigger,
} from "@renderer/components/ui/menubar";
import { BrushIcon, CameraIcon, RotateCcwIcon, SaveIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import type {
  ModelViewerThreeEnvironment,
  ModelViewerThreeToneMapping,
  ModelViewerToonStyle,
} from "./model-viewer-contract";

import { DEFAULT_MODEL_VIEWER_TOON_STYLE, MODEL_VIEWER_TOON_RANGES } from "./model-viewer-contract";
import {
  DEFAULT_THREE_EXPOSURE,
  MAX_THREE_EXPOSURE,
  MIN_THREE_EXPOSURE,
  MODEL_ROTATION_ACTIONS,
} from "./model-viewer-dialog-types";
import { formatSliderValue } from "./model-viewer-dialog-variants";

const TOON_NUMBER_FIELDS = [
  { key: "fieldOfView", label: "toon_field_of_view" },
  { key: "brightness", label: "toon_brightness" },
  { key: "lightStrength", label: "toon_light_strength" },
  { key: "ambient", label: "toon_ambient" },
  { key: "rangeBias", label: "toon_range_bias" },
  { key: "softness", label: "toon_softness" },
  { key: "shadowBrightness", label: "toon_shadow_brightness" },
  { key: "shadowSaturation", label: "toon_shadow_saturation" },
  { key: "specular", label: "toon_specular" },
  { key: "rimStrength", label: "toon_rim_strength" },
  { key: "rimWidth", label: "toon_rim_width" },
  { key: "outlineWidth", label: "toon_outline_width" },
  { key: "outlineBrightness", label: "toon_outline_brightness" },
] as const;

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
  showToonShadows?: boolean;
  toonShadows?: boolean;
  onToonShadowsChange?: (value: boolean) => void;
  showToonStyle?: boolean;
  toonStyle?: ModelViewerToonStyle;
  onToonStyleChange?: (value: ModelViewerToonStyle) => void;
  showNormalMaps?: boolean;
  normalMaps?: boolean;
  onNormalMapsChange?: (value: boolean) => void;

  /* Toggle (variant mode only) */
  showToggleViewer: boolean;
  isViewerBusy: boolean;
  onSaveTogglesToIni: () => void;
  onResetToggles: () => void;
  showOverlayInViewer?: boolean;
  overlayInViewer?: boolean;
  onOverlayInViewerChange?: (value: boolean) => void;
  canResetOverlayPlacement?: boolean;
  onResetOverlayPlacement?: () => void;

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
  showToonShadows = false,
  toonShadows = false,
  onToonShadowsChange,
  showToonStyle = false,
  toonStyle = DEFAULT_MODEL_VIEWER_TOON_STYLE,
  onToonStyleChange,
  showNormalMaps = false,
  normalMaps = true,
  onNormalMapsChange,
  showToggleViewer,
  isViewerBusy,
  onSaveTogglesToIni,
  onResetToggles,
  showOverlayInViewer = false,
  overlayInViewer = false,
  onOverlayInViewerChange,
  canResetOverlayPlacement = false,
  onResetOverlayPlacement,
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
            {showToonShadows ? (
              <>
                <MenubarSeparator />
                <MenubarGroup>
                  <MenubarCheckboxItem
                    checked={toonShadows}
                    onCheckedChange={(checked) => onToonShadowsChange?.(checked === true)}
                  >
                    Toon Shadows
                  </MenubarCheckboxItem>
                  {showToonStyle ? (
                    <MenubarSub>
                      <MenubarSubTrigger>
                        {t("page.tools.model_viewer.menu.toon_style")}
                      </MenubarSubTrigger>
                      <MenubarSubContent className="max-h-[70vh] w-56 overflow-y-auto">
                        <MenubarGroup>
                          <MenubarCheckboxItem
                            checked={toonStyle.outlines}
                            onSelect={(event) => event.preventDefault()}
                            onCheckedChange={(checked) =>
                              onToonStyleChange?.({ ...toonStyle, outlines: checked === true })
                            }
                          >
                            {t("page.tools.model_viewer.menu.toon_outlines")}
                          </MenubarCheckboxItem>
                          <MenubarCheckboxItem
                            checked={toonStyle.selfShadow}
                            onSelect={(event) => event.preventDefault()}
                            onCheckedChange={(checked) =>
                              onToonStyleChange?.({ ...toonStyle, selfShadow: checked === true })
                            }
                          >
                            {t("page.tools.model_viewer.menu.toon_self_shadow")}
                          </MenubarCheckboxItem>
                          <MenubarCheckboxItem
                            checked={toonStyle.lightFollowsCamera}
                            onSelect={(event) => event.preventDefault()}
                            onCheckedChange={(checked) =>
                              onToonStyleChange?.({
                                ...toonStyle,
                                lightFollowsCamera: checked === true,
                              })
                            }
                          >
                            {t("page.tools.model_viewer.menu.toon_light_follows_camera")}
                          </MenubarCheckboxItem>
                          <MenubarCheckboxItem
                            checked={toonStyle.flatFace}
                            onSelect={(event) => event.preventDefault()}
                            onCheckedChange={(checked) =>
                              onToonStyleChange?.({ ...toonStyle, flatFace: checked === true })
                            }
                          >
                            {t("page.tools.model_viewer.menu.toon_flat_face")}
                          </MenubarCheckboxItem>
                        </MenubarGroup>
                        <MenubarSeparator />
                        {TOON_NUMBER_FIELDS.map((field) => (
                          <div key={field.key}>
                            <MenubarLabel className="text-xs text-muted-foreground">
                              {t(`page.tools.model_viewer.menu.${field.label}`)}
                            </MenubarLabel>
                            <div className="px-1.5 py-1">
                              <Input
                                type="number"
                                {...MODEL_VIEWER_TOON_RANGES[field.key]}
                                value={formatSliderValue(toonStyle[field.key])}
                                onChange={(event) => {
                                  const range = MODEL_VIEWER_TOON_RANGES[field.key];
                                  const value = Number.parseFloat(event.target.value);
                                  if (Number.isFinite(value)) {
                                    onToonStyleChange?.({
                                      ...toonStyle,
                                      [field.key]: Math.min(Math.max(value, range.min), range.max),
                                    });
                                  }
                                }}
                              />
                            </div>
                          </div>
                        ))}
                        <MenubarSeparator />
                        <MenubarItem
                          onSelect={(event) => event.preventDefault()}
                          onClick={() => onToonStyleChange?.(DEFAULT_MODEL_VIEWER_TOON_STYLE)}
                        >
                          <RotateCcwIcon />
                          {t("page.tools.model_viewer.menu.reset")}
                        </MenubarItem>
                      </MenubarSubContent>
                    </MenubarSub>
                  ) : null}
                </MenubarGroup>
              </>
            ) : null}
            {showNormalMaps ? (
              <>
                <MenubarSeparator />
                <MenubarGroup>
                  <MenubarCheckboxItem
                    checked={normalMaps}
                    onCheckedChange={(checked) => onNormalMapsChange?.(checked === true)}
                  >
                    {t("page.tools.model_viewer.menu.normal_maps")}
                  </MenubarCheckboxItem>
                </MenubarGroup>
              </>
            ) : null}
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
            {showOverlayInViewer ? (
              <>
                <MenubarSeparator />
                <MenubarGroup>
                  <MenubarCheckboxItem
                    checked={overlayInViewer}
                    title={t("page.tools.model_viewer.menu.overlay_in_viewer_hint")}
                    onCheckedChange={(checked) => onOverlayInViewerChange?.(checked === true)}
                  >
                    {t("page.tools.model_viewer.menu.overlay_in_viewer")}
                  </MenubarCheckboxItem>
                  {overlayInViewer ? (
                    <MenubarItem
                      disabled={!canResetOverlayPlacement}
                      onClick={onResetOverlayPlacement}
                    >
                      <RotateCcwIcon />
                      {t("page.tools.model_viewer.menu.overlay_reset_placement")}
                    </MenubarItem>
                  ) : null}
                </MenubarGroup>
              </>
            ) : null}
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
