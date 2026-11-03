import { Dialog } from "@bindings/platform";
import { Tools } from "@bindings/tools";
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
import type { ViewerMaterialProfile } from "@shared/mod-viewer/types";
import { FolderOpenIcon, XIcon } from "lucide-react";
import type { ComponentType } from "react";
import { useTranslation } from "react-i18next";

import type { GIMIToonStyle } from "./gimi";
import type { ModelViewerToonStyleBase } from "./profile";

import { GENSHIN_FACE_TYPES } from "./gimi";
import { materialProfileOf } from "./registry";

type ToonMenuExtrasProps = {
  style: ModelViewerToonStyleBase;
  onStyleChange: (style: ModelViewerToonStyleBase) => void;
  assets: unknown;
};

export type ModelViewerToonAssetSource = {
  /** Dialog role under which the folder picker remembers its last folder. */
  dialogRole: string;
  folderLabel: string;
  clearLabel: string;
  failedLabel: string;
  list(dir: string): PromiseLike<unknown> & { cancel(): unknown };
};

// Menu parts live apart from the render modules so the grid-preview worker
// never loads React or Wails runtime bindings.
type MaterialProfileUI = {
  /** Key under page.tools.model_viewer.menu; defaults to toon_style. */
  label?: string;
  /** Lists game assets from the folder stored in the style's assetDir. */
  assets?: ModelViewerToonAssetSource;
  /** Rendered after the folder items once assets are listed. */
  MenuExtras?: ComponentType<ToonMenuExtrasProps>;
};

const materialProfileUIs: Partial<Record<ViewerMaterialProfile, MaterialProfileUI>> = {
  gimi: {
    label: "gimi_toon_style",
    assets: {
      dialogRole: "model-viewer-genshin-textures",
      folderLabel: "gimi_toon_assets_folder",
      clearLabel: "gimi_toon_assets_clear",
      failedLabel: "gimi_toon_assets_failed",
      list: (dir) => Tools.ListGenshinToonAssets(dir),
    },
    MenuExtras: GIMIToonMenuExtras,
  },
  srmi: { label: "srmi_toon_style" },
  zzmi: { label: "zzmi_toon_style" },
};

export function toonAssetSourceOf(
  id: ViewerMaterialProfile | undefined,
): ModelViewerToonAssetSource | undefined {
  return id ? materialProfileUIs[id]?.assets : undefined;
}

export function ModelViewerToonMenu({
  profile: id,
  style,
  onStyleChange,
  assets,
}: {
  profile: ViewerMaterialProfile;
  style: ModelViewerToonStyleBase;
  onStyleChange: (style: ModelViewerToonStyleBase) => void;
  assets?: unknown;
}) {
  const { t } = useTranslation();
  const flags = materialProfileOf(id).toon?.flags ?? [];
  const ui = materialProfileUIs[id];
  const assetDir = style.assetDir ?? "";

  const selectAssetDir = async () => {
    if (!ui?.assets) return;
    const selected = await Dialog.ShowOpenDialog({
      title: "",
      defaultPath: assetDir,
      filters: [],
      properties: ["openDirectory"],
      role: ui.assets.dialogRole,
    });
    if (selected.canceled || !selected.filePaths?.[0]) return;
    onStyleChange({ ...style, assetDir: selected.filePaths[0] });
  };

  return (
    <MenubarSub>
      <MenubarSubTrigger>
        {t(`page.tools.model_viewer.menu.${ui?.label ?? "toon_style"}`)}
      </MenubarSubTrigger>
      <MenubarSubContent className={ui ? "max-h-[70vh] w-60 overflow-y-auto" : "w-56"}>
        <MenubarGroup>
          {flags.map((flag) => (
            <MenubarCheckboxItem
              key={flag.key}
              checked={(style as Record<string, unknown>)[flag.key] === true}
              onSelect={(event) => event.preventDefault()}
              onCheckedChange={(checked) =>
                onStyleChange({ ...style, [flag.key]: checked === true })
              }
            >
              {t(`page.tools.model_viewer.menu.${flag.label}`)}
            </MenubarCheckboxItem>
          ))}
        </MenubarGroup>
        {ui?.assets ? (
          <>
            <MenubarSeparator />
            <MenubarItem onClick={() => void selectAssetDir()}>
              <FolderOpenIcon />
              {t(`page.tools.model_viewer.menu.${ui.assets.folderLabel}`)}
            </MenubarItem>
            {assetDir ? (
              <>
                <MenubarLabel
                  className="truncate text-[11px] font-normal text-muted-foreground"
                  title={assetDir}
                >
                  {assetDir}
                </MenubarLabel>
                <MenubarItem
                  onSelect={(event) => event.preventDefault()}
                  onClick={() => onStyleChange({ ...style, assetDir: "" })}
                >
                  <XIcon />
                  {t(`page.tools.model_viewer.menu.${ui.assets.clearLabel}`)}
                </MenubarItem>
              </>
            ) : null}
          </>
        ) : null}
        {ui?.MenuExtras && assets ? (
          <ui.MenuExtras style={style} onStyleChange={onStyleChange} assets={assets} />
        ) : null}
      </MenubarSubContent>
    </MenubarSub>
  );
}

function GIMIToonMenuExtras({ style: baseStyle, onStyleChange, assets }: ToonMenuExtrasProps) {
  const { t } = useTranslation();
  const style = baseStyle as GIMIToonStyle;
  const listing = assets as GenshinToonAssetsTransport;
  const ramps = listing.ramps ?? [];
  const update = (patch: Partial<GIMIToonStyle>) => onStyleChange({ ...style, ...patch });

  return (
    <>
      <MenubarSeparator />
      <MenubarLabel className="text-xs text-muted-foreground">
        {t("page.tools.model_viewer.menu.gimi_toon_ramp")}
      </MenubarLabel>
      <MenubarRadioGroup value={style.ramp} onValueChange={(ramp) => update({ ramp })}>
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
        onValueChange={(faceType) => update({ faceType: faceType as GIMIToonStyle["faceType"] })}
      >
        {(["auto", ...GENSHIN_FACE_TYPES] as const).map((faceType) => (
          <MenubarRadioItem
            key={faceType}
            value={faceType}
            disabled={faceType !== "auto" && !listing.faceLightmaps?.[faceType]}
            onSelect={(event) => event.preventDefault()}
          >
            {t(`page.tools.model_viewer.menu.gimi_toon_face_${faceType}`)}
          </MenubarRadioItem>
        ))}
      </MenubarRadioGroup>
    </>
  );
}
