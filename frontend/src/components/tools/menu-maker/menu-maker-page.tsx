import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { cn } from "@renderer/lib/utils";
import {
  calculateMenuMakerPreviewScale,
  menuMakerColumnCount,
  mergeMenuMakerSlots,
} from "@shared/menu-maker/generator";
import { createStateToken } from "@shared/menu-maker/icons";
import { suggestMenuMakerSlotIcon } from "@shared/menu-maker/parser";
import {
  emptyMenuMakerGeometry,
  MENU_MAKER_BASE_PANEL_IMAGE_SIZE,
  MENU_MAKER_BASE_SLOT_SIZE,
  menuMakerTitleText,
} from "@shared/menu-maker/types";
import {
  ArchiveIcon,
  BanIcon,
  FileCodeIcon,
  FolderOpenIcon,
  HistoryIcon,
  ImageIcon,
  Loader2Icon,
  MergeIcon,
  PencilIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  SaveIcon,
  UploadIcon,
  WandSparklesIcon,
  XIcon,
} from "lucide-react";
import { useState } from "react";

import { DraftDialog, CompareDialog } from "./menu-maker-dialogs";
import { alphaColor, SettingSelect } from "./menu-maker-editor-panels";
import { Inspector, PreviewSlot, SlotEditor, EmptyState } from "./menu-maker-editor-panels";
import { IconPicker, CropDialog } from "./menu-maker-media-dialogs";
import { useMenuMakerEditor } from "./use-menu-maker-editor";

interface MenuMakerPageProps {
  path: string;
  name: string;
  ini: string;
}
export function MenuMakerPage({ path, name, ini }: MenuMakerPageProps) {
  const [selected, setSelected] = useState<string[]>([]);
  const [mergeMode, setMergeMode] = useState<"allKeys" | "guiOnly">("guiOnly");
  const [dialog, setDialog] = useState<"icon" | "crop" | "drafts" | "compare" | null>(null);
  const [editingSlotId, setEditingSlotId] = useState<string>();
  const [cropSource, setCropSource] = useState<string>();
  const [cropTarget, setCropTarget] = useState<"slot" | "panel">("slot");
  const [narrowPanel, setNarrowPanel] = useState<"inspector" | "preview" | "editor" | "ini">(
    "preview",
  );
  const [sidePanel, setSidePanel] = useState<"editor" | "ini">("editor");
  const {
    t,
    state,
    dispatch,
    preview,
    chooseFile,
    chooseFolder,
    applyBundle,
    saveINI,
    saveZIP,
    setSlot,
    moveSlot,
    loadSource,
    scanFolder,
    restoreDraft,
  } = useMenuMakerEditor({
    path,
    ini,
    onSourceLoaded: () => setSelected([]),
    onDraftRestored: () => setDialog(null),
  });
  const generatedINI = preview?.iniText ?? "";
  const geometry = preview?.geometry ?? emptyMenuMakerGeometry();
  const previewResolutionScale = calculateMenuMakerPreviewScale(state.settings);
  const previewTitle = menuMakerTitleText(state.settings);
  const startUpload = (slotId: string) => {
    startImageCrop("slot", slotId);
  };
  const startImageCrop = (target: "slot" | "panel", slotId?: string) => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => {
      const file = input.files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = () => {
        if (typeof reader.result !== "string") return;
        setEditingSlotId(slotId);
        setCropTarget(target);
        setCropSource(reader.result);
        setDialog("crop");
      };
      reader.readAsDataURL(file);
    };
    input.click();
  };
  const empty = !state.document;
  return (
    <div className="flex h-full min-h-0 flex-col bg-background text-foreground">
      <header className="flex flex-wrap items-center gap-2 border-b border-border p-3">
        <div className="mr-auto min-w-0">
          <h1 className="truncate text-sm font-semibold">{t("page.tools.menu_maker.title")}</h1>
          {(state.source?.path || name) && (
            <p className="truncate text-xs text-muted-foreground">
              {state.source?.path || `${name} · ${path}`}
            </p>
          )}
        </div>
        {state.scan && (
          <Badge variant="secondary" className="rounded-md px-2 py-1 text-[11px] font-normal">
            INI {state.scan.stats.ini} · TXT {state.scan.stats.txt} ·{" "}
            {t("page.tools.menu_maker.excluded")} {state.scan.stats.disabled}
          </Badge>
        )}
        <ButtonGroup className="flex-wrap">
          <ButtonGroup>
            <Button variant="outline" size="sm" onClick={() => void chooseFile()}>
              <FileCodeIcon />
              {t("page.tools.menu_maker.choose_file")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => void chooseFolder()}>
              <FolderOpenIcon />
              {t("page.tools.menu_maker.choose_folder")}
            </Button>
          </ButtonGroup>
          <ButtonGroup>
            <Button variant="outline" size="sm" onClick={() => setDialog("drafts")}>
              <HistoryIcon />
              {t("page.tools.menu_maker.drafts")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={empty}
              onClick={() => setDialog("compare")}
            >
              <RefreshCwIcon />
              {t("page.tools.menu_maker.compare")}
            </Button>
          </ButtonGroup>
          <ButtonGroup>
            <Button
              variant="outline"
              size="sm"
              disabled={empty || state.busy}
              onClick={() => void saveINI()}
            >
              <SaveIcon />
              INI
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={empty || state.busy}
              onClick={() => void saveZIP()}
            >
              <ArchiveIcon />
              ZIP
            </Button>
          </ButtonGroup>
          <ButtonGroup>
            <Button
              size="sm"
              disabled={empty || state.busy || !state.sourceAvailable}
              onClick={() => void applyBundle()}
            >
              {state.busy ? <Loader2Icon className="animate-spin" /> : <UploadIcon />}
              {t("page.tools.menu_maker.apply")}
            </Button>
          </ButtonGroup>
        </ButtonGroup>
      </header>

      <div className="border-b border-border p-1 lg:hidden">
        <ButtonGroup className="w-full">
          {(["inspector", "preview", "editor", "ini"] as const).map((panel) => (
            <Button
              key={panel}
              variant={narrowPanel === panel ? "secondary" : "ghost"}
              size="sm"
              className="flex-1"
              onClick={() => {
                setNarrowPanel(panel);
                if (panel === "editor" || panel === "ini") setSidePanel(panel);
              }}
            >
              {t(`page.tools.menu_maker.${panel}`)}
            </Button>
          ))}
        </ButtonGroup>
      </div>

      <main className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[290px_minmax(360px,1fr)_minmax(300px,38%)]">
        <aside
          className={cn(
            "scroll-area overflow-y-auto border-r border-border p-3",
            narrowPanel !== "inspector" && "hidden lg:block",
          )}
        >
          <Inspector settings={state.settings} dispatch={dispatch} />
          <div className="mt-4 flex gap-2">
            <Button
              variant="outline"
              size="sm"
              className="flex-1"
              onClick={() => startImageCrop("panel")}
            >
              <ImageIcon />
              {t("page.tools.menu_maker.panel_image")}
            </Button>
            {state.settings.panelImageDataUrl && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() =>
                  dispatch({ type: "settings", value: { panelImageDataUrl: undefined } })
                }
              >
                <XIcon />
              </Button>
            )}
          </div>
        </aside>

        <section
          className={cn(
            "scroll-area min-w-0 overflow-auto bg-muted/20 p-4",
            narrowPanel !== "preview" && "hidden lg:block",
          )}
        >
          {empty ? (
            <EmptyState
              scan={state.scan}
              onLoad={loadSource}
              onIncludeTXT={() =>
                state.scan?.rootPath && void scanFolder(state.scan.rootPath, true)
              }
              t={t}
            />
          ) : (
            <div
              className="mx-auto"
              style={{
                width: geometry.panelWidth * previewResolutionScale,
                height: geometry.panelHeight * previewResolutionScale,
              }}
            >
              <div
                className="rounded"
                style={{
                  width: geometry.panelWidth,
                  height: geometry.panelHeight,
                  transform: `scale(${previewResolutionScale})`,
                  transformOrigin: "top left",
                  backgroundColor: alphaColor(
                    state.settings.palette.panelBackground,
                    state.settings.palette.panelBackgroundAlpha,
                  ),
                  backgroundImage: state.settings.panelImageDataUrl
                    ? `url(${state.settings.panelImageDataUrl})`
                    : undefined,
                  backgroundSize: "cover",
                  boxShadow: `inset 0 0 0 2px ${alphaColor(state.settings.palette.panelBorder, state.settings.palette.panelBorderAlpha)}, 0 25px 50px -12px rgb(0 0 0 / 0.25)`,
                }}
              >
                {previewTitle ? (
                  <h2
                    className="flex truncate px-4 font-semibold"
                    style={{
                      height: geometry.titleHeight,
                      alignItems: "center",
                      color: state.settings.palette.title,
                      textShadow: `0 1px 3px ${state.settings.palette.titleShadow}`,
                    }}
                  >
                    {previewTitle}
                  </h2>
                ) : null}
                <div
                  className="grid"
                  style={{
                    gridTemplateColumns: `repeat(${menuMakerColumnCount(state.settings.columns)}, ${geometry.slotSize}px)`,
                    gap: geometry.scaledGap,
                    padding: `${geometry.padding}px ${geometry.padding}px ${geometry.padding}px`,
                  }}
                >
                  {state.slots
                    .filter((slot) => !slot.skip)
                    .map((slot) => (
                      <PreviewSlot
                        key={slot.id}
                        slot={slot}
                        size={geometry.slotSize}
                        settings={state.settings}
                      />
                    ))}
                </div>
              </div>
            </div>
          )}
        </section>

        <aside
          className={cn(
            "min-h-0 flex-col border-l border-border",
            narrowPanel === "editor" || narrowPanel === "ini" ? "flex" : "hidden lg:flex",
          )}
        >
          <div className="border-b border-border p-1">
            <ButtonGroup className="w-full">
              <Button
                variant={sidePanel === "editor" ? "secondary" : "ghost"}
                size="sm"
                className="min-w-0 flex-1"
                onClick={() => {
                  setSidePanel("editor");
                  setNarrowPanel("editor");
                }}
              >
                <PencilIcon />
                {t("page.tools.menu_maker.editor")}
              </Button>
              <Button
                variant={sidePanel === "ini" ? "secondary" : "ghost"}
                size="sm"
                className="min-w-0 flex-1"
                onClick={() => {
                  setSidePanel("ini");
                  setNarrowPanel("ini");
                }}
              >
                <FileCodeIcon />
                {t("page.tools.menu_maker.ini_preview")}
              </Button>
            </ButtonGroup>
          </div>
          {sidePanel === "editor" ? (
            <div className="scroll-area min-h-0 flex-1 overflow-auto p-3">
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {state.slots.length} {t("page.tools.menu_maker.slots")}
                </span>
                <SettingSelect
                  value={mergeMode}
                  items={[
                    { value: "guiOnly", label: t("page.tools.menu_maker.merge_gui_only") },
                    { value: "allKeys", label: t("page.tools.menu_maker.merge_all_keys") },
                  ]}
                  onChange={setMergeMode}
                  disabled={empty}
                  size="sm"
                />
                <ButtonGroup>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={selected.length < 2}
                    onClick={() => {
                      dispatch({
                        type: "slots",
                        value: mergeMenuMakerSlots(state.slots, selected, mergeMode),
                      });
                      setSelected([]);
                    }}
                  >
                    <MergeIcon />
                    {t("page.tools.menu_maker.merge")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!selected.length}
                    onClick={() =>
                      dispatch({
                        type: "slots",
                        value: state.slots.map((slot) =>
                          selected.includes(slot.id) ? { ...slot, skip: true } : slot,
                        ),
                      })
                    }
                  >
                    <BanIcon />
                    {t("page.tools.menu_maker.skip")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!selected.length}
                    onClick={() =>
                      dispatch({
                        type: "slots",
                        value: state.slots.map((slot) =>
                          selected.includes(slot.id)
                            ? {
                                ...slot,
                                skip: false,
                                icon: { kind: "lucide", name: "circle-dot", color: "#ff4fb3" },
                              }
                            : slot,
                        ),
                      })
                    }
                  >
                    <RotateCcwIcon />
                    {t("page.tools.menu_maker.reset")}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={!selected.length}
                    onClick={() =>
                      dispatch({
                        type: "slots",
                        value: state.slots.map((slot) =>
                          selected.includes(slot.id)
                            ? { ...slot, icon: suggestMenuMakerSlotIcon(slot) }
                            : slot,
                        ),
                      })
                    }
                  >
                    <WandSparklesIcon />
                    {t("page.tools.menu_maker.auto_icon")}
                  </Button>
                </ButtonGroup>
              </div>
              {empty ? (
                <p className="text-xs text-muted-foreground">
                  {t("page.tools.menu_maker.no_preview")}
                </p>
              ) : (
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {state.slots.map((slot) => (
                    <SlotEditor
                      key={slot.id}
                      slot={slot}
                      selected={selected.includes(slot.id)}
                      settings={state.settings}
                      onSelect={(checked) =>
                        setSelected((items) =>
                          checked ? [...items, slot.id] : items.filter((id) => id !== slot.id),
                        )
                      }
                      onChange={(transform) => setSlot(slot.id, transform)}
                      onMove={(draggedId) => moveSlot(draggedId, slot.id)}
                      onIcon={() => {
                        setEditingSlotId(slot.id);
                        setDialog("icon");
                      }}
                      onUpload={() => startUpload(slot.id)}
                    />
                  ))}
                </div>
              )}
            </div>
          ) : (
            <pre className="scroll-area min-h-0 flex-1 overflow-auto p-3 font-mono text-[11px] leading-5 whitespace-pre select-text">
              {generatedINI || t("page.tools.menu_maker.no_preview")}
            </pre>
          )}
        </aside>
      </main>

      {dialog === "icon" && editingSlotId && (
        <IconPicker
          currentToken={createStateToken(state.source?.sha256 ?? "", editingSlotId, state.revision)}
          onClose={() => setDialog(null)}
          onPick={(icon) => {
            setSlot(editingSlotId, (slot) => ({ ...slot, icon }));
            setDialog(null);
          }}
          t={t}
        />
      )}
      {dialog === "crop" && cropSource && (cropTarget === "panel" || editingSlotId) && (
        <CropDialog
          source={cropSource}
          size={
            cropTarget === "panel"
              ? Math.max(
                  MENU_MAKER_BASE_PANEL_IMAGE_SIZE,
                  Math.round(
                    (MENU_MAKER_BASE_PANEL_IMAGE_SIZE * geometry.slotSize) /
                      MENU_MAKER_BASE_SLOT_SIZE,
                  ),
                )
              : geometry.slotSize
          }
          onClose={() => setDialog(null)}
          onConfirm={(dataUrl) => {
            if (cropTarget === "panel") {
              dispatch({ type: "settings", value: { panelImageDataUrl: dataUrl } });
            } else if (editingSlotId) {
              setSlot(editingSlotId, (slot) => ({
                ...slot,
                icon: { kind: "upload", name: "upload", color: "#ffffff", dataUrl },
              }));
            }
            setDialog(null);
          }}
          t={t}
        />
      )}
      {dialog === "drafts" && (
        <DraftDialog onRestore={restoreDraft} onClose={() => setDialog(null)} t={t} />
      )}
      {dialog === "compare" && state.source && (
        <CompareDialog
          original={state.source.text}
          generated={generatedINI}
          onClose={() => setDialog(null)}
          t={t}
        />
      )}
    </div>
  );
}
