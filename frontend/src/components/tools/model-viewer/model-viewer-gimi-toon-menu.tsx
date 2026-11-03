import type { GenshinToonAssetsTransport } from "@bindings/tools/model_viewer";
import {
  MenubarCheckboxItem,
  MenubarGroup,
  MenubarItem,
  MenubarLabel,
  MenubarRadioGroup,
  MenubarRadioItem,
  MenubarSeparator,
  MenubarSub,
  MenubarSubContent,
  MenubarSubTrigger,
} from "@renderer/components/ui/menubar";
import { FolderOpenIcon, XIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import type { ModelViewerGIMIToonStyle } from "./model-viewer-contract";

import { GENSHIN_FACE_TYPES } from "./model-viewer-contract";

const FLAGS = [
  { key: "outlines", label: "toon_outlines" },
  { key: "selfShadow", label: "toon_self_shadow" },
  { key: "lightFollowsCamera", label: "toon_light_follows_camera" },
] as const;

export function ModelViewerGIMIToonMenu({
  style,
  onStyleChange,
  assets,
  assetDir,
  onSelectAssetDir,
  onClearAssetDir,
}: {
  style: ModelViewerGIMIToonStyle;
  onStyleChange?: (value: ModelViewerGIMIToonStyle) => void;
  assets?: GenshinToonAssetsTransport;
  assetDir: string;
  onSelectAssetDir?: () => void;
  onClearAssetDir?: () => void;
}) {
  const { t } = useTranslation();
  const ramps = assets?.ramps ?? [];

  return (
    <MenubarSub>
      <MenubarSubTrigger>{t("page.tools.model_viewer.menu.gimi_toon_style")}</MenubarSubTrigger>
      <MenubarSubContent className="max-h-[70vh] w-60 overflow-y-auto">
        <MenubarGroup>
          {FLAGS.map((flag) => (
            <MenubarCheckboxItem
              key={flag.key}
              checked={style[flag.key]}
              onSelect={(event) => event.preventDefault()}
              onCheckedChange={(checked) =>
                onStyleChange?.({ ...style, [flag.key]: checked === true })
              }
            >
              {t(`page.tools.model_viewer.menu.${flag.label}`)}
            </MenubarCheckboxItem>
          ))}
        </MenubarGroup>
        <MenubarSeparator />
        <MenubarItem onClick={onSelectAssetDir}>
          <FolderOpenIcon />
          {t("page.tools.model_viewer.menu.gimi_toon_assets_folder")}
        </MenubarItem>
        {assetDir ? (
          <>
            <MenubarLabel
              className="truncate text-[11px] font-normal text-muted-foreground"
              title={assetDir}
            >
              {assetDir}
            </MenubarLabel>
            <MenubarItem onSelect={(event) => event.preventDefault()} onClick={onClearAssetDir}>
              <XIcon />
              {t("page.tools.model_viewer.menu.gimi_toon_assets_clear")}
            </MenubarItem>
          </>
        ) : null}
        {assets ? (
          <>
            <MenubarSeparator />
            <MenubarLabel className="text-xs text-muted-foreground">
              {t("page.tools.model_viewer.menu.gimi_toon_ramp")}
            </MenubarLabel>
            <MenubarRadioGroup
              value={style.ramp}
              onValueChange={(ramp) => onStyleChange?.({ ...style, ramp })}
            >
              <MenubarRadioItem value="" onSelect={(event) => event.preventDefault()}>
                {t("page.tools.model_viewer.menu.gimi_toon_ramp_builtin")}
              </MenubarRadioItem>
              {ramps.map((ramp) => (
                <MenubarRadioItem
                  key={ramp.name}
                  value={ramp.name}
                  title={ramp.name}
                  onSelect={(event) => event.preventDefault()}
                >
                  <span className="truncate">{ramp.name.replace(/^avatar_/i, "")}</span>
                </MenubarRadioItem>
              ))}
            </MenubarRadioGroup>
            <MenubarSeparator />
            <MenubarLabel className="text-xs text-muted-foreground">
              {t("page.tools.model_viewer.menu.gimi_toon_face_type")}
            </MenubarLabel>
            <MenubarRadioGroup
              value={style.faceType}
              onValueChange={(faceType) =>
                onStyleChange?.({
                  ...style,
                  faceType: faceType as ModelViewerGIMIToonStyle["faceType"],
                })
              }
            >
              {(["auto", ...GENSHIN_FACE_TYPES] as const).map((faceType) => (
                <MenubarRadioItem
                  key={faceType}
                  value={faceType}
                  disabled={faceType !== "auto" && !assets.faceLightmaps?.[faceType]}
                  onSelect={(event) => event.preventDefault()}
                >
                  {t(`page.tools.model_viewer.menu.gimi_toon_face_${faceType}`)}
                </MenubarRadioItem>
              ))}
            </MenubarRadioGroup>
          </>
        ) : null}
      </MenubarSubContent>
    </MenubarSub>
  );
}
