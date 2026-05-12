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
import { CameraIcon, RotateCcwIcon, SaveIcon } from "lucide-react";
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
    </Menubar>
  );
}
