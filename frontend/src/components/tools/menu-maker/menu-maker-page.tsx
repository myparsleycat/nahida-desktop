import type {
  MenuMakerDocument,
  MenuMakerGenerateResult,
  MenuMakerScanResult,
  MenuMakerSource,
} from "@bindings/menumaker";
import { MenuMaker } from "@bindings/menumaker";
import { Dialog } from "@bindings/platform";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Dialog as ModalDialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldLabel, FieldLegend, FieldSet } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Switch } from "@renderer/components/ui/switch";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { drawCoveredImage, MENU_MAKER_CROP_PREVIEW_SIZE } from "@shared/menu-maker/crop";
import {
  canRestoreDraft,
  deleteDraftBlobs,
  detachDraftMedia,
  loadDraftBlobs,
  loadDraftMetadata,
  restoreDraftMedia,
  saveDraftBlobs,
  saveDraftMetadata,
} from "@shared/menu-maker/drafts";
import {
  calculateMenuMakerPreviewScale,
  menuMakerColumnCount,
  mergeMenuMakerSlots,
  moveMenuMakerSlot,
} from "@shared/menu-maker/generator";
import {
  acceptsIconResult,
  clearIconifyCache,
  createStateToken,
  deleteIconifyPrefix,
  downloadIconifyCollection,
  getFavoriteIconifyPrefixes,
  getIconifyCacheStats,
  sanitizeIconifySVG,
  searchCachedIconifyIcons,
  searchIconifyIcons,
  searchLucideIcons,
  toggleFavoriteIconifyPrefix,
  type IconSearchResult,
  type IconifyCacheStats,
} from "@shared/menu-maker/icons";
import {
  slotSignature,
  suggestMenuMakerSlotIcon,
  updateSlotKey,
  withSuggestedIcons,
} from "@shared/menu-maker/parser";
import { renderMenuMakerAssets } from "@shared/menu-maker/resources";
import {
  DEFAULT_MENU_MAKER_SETTINGS,
  emptyMenuMakerGeometry,
  MENU_MAKER_BASE_PANEL_IMAGE_SIZE,
  MENU_MAKER_BASE_SLOT_SIZE,
  menuMakerTitleText,
  type MenuMakerSettings,
  type MenuMakerSlot,
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
  SearchIcon,
  StarIcon,
  UploadIcon,
  WandSparklesIcon,
  XIcon,
} from "lucide-react";
import { DynamicIcon, iconNames } from "lucide-react/dynamic";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface MenuMakerPageProps {
  path: string;
  name: string;
  ini: string;
}

interface EditorState {
  source?: MenuMakerSource;
  document?: MenuMakerDocument;
  slots: MenuMakerSlot[];
  settings: MenuMakerSettings;
  scan?: MenuMakerScanResult;
  sourceAvailable: boolean;
  busy: boolean;
  revision: number;
}

type EditorAction =
  | { type: "busy"; value: boolean }
  | { type: "scan"; value: MenuMakerScanResult }
  | {
      type: "load";
      source: MenuMakerSource;
      document: MenuMakerDocument;
      slots?: MenuMakerSlot[];
      settings?: MenuMakerSettings;
      sourceAvailable?: boolean;
    }
  | { type: "sourceAvailable"; value: boolean }
  | { type: "sourceContent"; text: string; sha256: string }
  | { type: "slots"; value: MenuMakerSlot[] }
  | { type: "settings"; value: Partial<MenuMakerSettings> }
  | { type: "palette"; key: keyof MenuMakerSettings["palette"]; value: string | number };

const initialState: EditorState = {
  slots: [],
  settings: loadStoredSettings(),
  sourceAvailable: false,
  busy: false,
  revision: 0,
};

export function MenuMakerPage({ path, name, ini }: MenuMakerPageProps) {
  const { t } = useTranslation();
  const [state, dispatch] = useReducer(reducer, initialState);
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
  const [preview, setPreview] = useState<MenuMakerGenerateResult>();
  const initialized = useRef(false);

  const generatedINI = preview?.iniText ?? "";
  const geometry = preview?.geometry ?? emptyMenuMakerGeometry();
  const previewResolutionScale = calculateMenuMakerPreviewScale(state.settings);
  const previewTitle = menuMakerTitleText(state.settings);

  const loadSource = useCallback(
    async (filePath: string) => {
      dispatch({ type: "busy", value: true });
      try {
        const source = await MenuMaker.LoadSource(filePath);
        const document = source.document;
        const draft = loadDraftMetadata().find((item) =>
          canRestoreDraft(item, source.sha256, slotSignature(document.slots ?? [])),
        );
        const blobs = draft
          ? await loadDraftBlobs(draft.id).catch((error) => {
              Logger.error({ error, draftId: draft.id }, "MenuMakerPage:loadDraftBlobs");
              return undefined;
            })
          : undefined;
        const restored = draft ? restoreDraftMedia(draft, blobs) : undefined;
        dispatch({
          type: "load",
          source,
          document,
          slots: restored?.slots ?? withSuggestedIcons(document.slots),
          settings: restored?.settings,
          sourceAvailable: true,
        });
        setSelected([]);
        toast.success(
          draft ? t("page.tools.menu_maker.draft_restored") : t("page.tools.menu_maker.loaded"),
        );
      } catch (error) {
        Logger.error({ error, filePath }, "MenuMakerPage:loadSource");
        toast.error(
          String(error).includes("MENU_MAKER_NO_KEY_SECTIONS")
            ? t("page.tools.menu_maker.no_keys")
            : t("page.tools.menu_maker.load_failed"),
        );
      } finally {
        dispatch({ type: "busy", value: false });
      }
    },
    [t],
  );

  const scanFolder = useCallback(
    async (root: string, includeTXT = false) => {
      dispatch({ type: "busy", value: true });
      try {
        const result = await MenuMaker.ScanFolder(root, includeTXT);
        dispatch({ type: "scan", value: result });
        if (result.files?.length === 1) await loadSource(result.files[0].path);
      } catch (error) {
        Logger.error({ error, root, includeTXT }, "MenuMakerPage:scanFolder");
        toast.error(t("page.tools.menu_maker.scan_failed"));
      } finally {
        dispatch({ type: "busy", value: false });
      }
    },
    [loadSource, t],
  );

  useEffect(() => {
    if (initialized.current) return;
    initialized.current = true;
    // Route search parameters are an external navigation input and must hydrate the editor once.
    // oxlint-disable-next-line react/set-state-in-effect
    if (ini) void loadSource(ini);
    else if (path) void scanFolder(path);
  }, [ini, loadSource, path, scanFolder]);

  const persistDraft = useCallback(() => {
    if (!state.source || !state.document) return;
    const source = state.source;
    const signature = slotSignature(state.document.slots ?? []);
    const id = `${source.sha256}:${signature}`;
    const detached = detachDraftMedia(state.settings, state.slots);
    const existing = loadDraftMetadata();
    const superseded = existing.filter(
      (draft) =>
        draft.id !== id && draft.sourcePath === source.path && draft.slotSignature === signature,
    );
    try {
      saveDraftMetadata([
        {
          id,
          sourcePath: source.path,
          sourceName: source.fileName,
          sourceSHA256: source.sha256,
          slotSignature: signature,
          updatedAt: Date.now(),
          sourceEncoding: source.encoding,
          sourceHasBOM: source.hasBOM,
          sourceNewline: source.newline,
          settings: detached.settings,
          slots: detached.slots,
        },
        ...existing.filter(
          (draft) => draft.id !== id && !superseded.some((item) => item.id === draft.id),
        ),
      ]);
    } catch (error) {
      Logger.error({ error, draftId: id }, "MenuMakerPage:saveDraftMetadata");
    }
    for (const draft of superseded) {
      void deleteDraftBlobs(draft.id).catch((error) =>
        Logger.error({ error, draftId: draft.id }, "MenuMakerPage:deleteDraftBlobs"),
      );
    }
    void saveDraftBlobs(id, { originalText: source.text, ...detached.blobs }).catch((error) =>
      Logger.error({ error, draftId: id }, "MenuMakerPage:saveDraftBlobs"),
    );
  }, [state.document, state.settings, state.slots, state.source]);

  useEffect(() => {
    if (!state.source || !state.document) return;
    const timeout = window.setTimeout(persistDraft, 500);
    return () => window.clearTimeout(timeout);
  }, [persistDraft, state.document, state.source]);

  useEffect(() => {
    window.addEventListener("beforeunload", persistDraft);
    return () => {
      persistDraft();
      window.removeEventListener("beforeunload", persistDraft);
    };
  }, [persistDraft]);

  useEffect(() => {
    localStorage.setItem(
      "nahida.menu-maker.settings",
      JSON.stringify({ ...state.settings, panelImageDataUrl: undefined }),
    );
  }, [state.settings]);

  useEffect(() => {
    if (!state.source?.text) return;
    let cancelled = false;
    void MenuMaker.Generate({
      sourceText: state.source.text,
      slots: state.slots,
      settings: state.settings,
    })
      .then((result) => {
        if (!cancelled) setPreview(result);
      })
      .catch((error) => {
        if (cancelled) return;
        Logger.error({ error, sourcePath: state.source?.path }, "MenuMakerPage:generate");
        setPreview(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [state.settings, state.slots, state.source]);

  const chooseFile = async () => {
    const result = await Dialog.ShowOpenDialog({
      title: t("page.tools.menu_maker.choose_file"),
      defaultPath: state.source?.path ?? path,
      filters: [{ name: "3DMigoto INI", extensions: ["ini", "txt"] }],
      properties: ["openFile"],
    });
    const selectedPath = result.filePaths?.[0];
    if (selectedPath) await loadSource(selectedPath);
  };

  const chooseFolder = async () => {
    const result = await Dialog.SelectDirectory();
    if (!result.canceled && result.filePath) await scanFolder(result.filePath);
  };

  const outputName = state.source
    ? buildOutputName(state.source.fileName, state.settings.useOriginalININame)
    : "menu_gui.ini";

  const generateCurrent = async () => {
    if (!state.source) return null;
    try {
      const result = await MenuMaker.Generate({
        sourceText: state.source.text,
        slots: state.slots,
        settings: state.settings,
      });
      setPreview(result);
      return result;
    } catch (error) {
      Logger.error({ error, sourcePath: state.source.path }, "MenuMakerPage:generate");
      toast.error(t("page.tools.menu_maker.load_failed"));
      return null;
    }
  };

  const buildAssets = async (generated: MenuMakerGenerateResult) => {
    try {
      return await renderMenuMakerAssets(
        state.slots,
        state.settings,
        generated.geometry,
        generated.slotStates,
      );
    } catch (error) {
      Logger.error({ error, sourcePath: state.source?.path }, "MenuMakerPage:renderAssets");
      toast.error(t("page.tools.menu_maker.font_failed"));
      return null;
    }
  };

  const applyBundle = async () => {
    if (!state.source || !state.sourceAvailable) return;
    dispatch({ type: "busy", value: true });
    try {
      const generated = await generateCurrent();
      if (!generated) return;
      const assets = await buildAssets(generated);
      if (!assets) return;
      const result = await MenuMaker.ApplyBundle({
        sourcePath: state.source.path,
        sourceSHA256: state.source.sha256,
        outputININame: outputName,
        slots: state.slots,
        settings: state.settings,
        encoding: state.source.encoding,
        hasBOM: state.source.hasBOM,
        newline: state.source.newline,
        assets,
        useOriginalININame: state.settings.useOriginalININame,
      });
      toast.success(t("page.tools.menu_maker.applied", { path: result.outputINIPath }));
      if (result.sourceSHA256) {
        dispatch({
          type: "sourceContent",
          text: generated.iniText,
          sha256: result.sourceSHA256,
        });
      } else if (/\.ini$/i.test(state.source.fileName) && !state.settings.useOriginalININame) {
        dispatch({ type: "sourceAvailable", value: false });
      }
    } catch (error) {
      Logger.error(
        { error, sourcePath: state.source.path, outputName },
        "MenuMakerPage:applyBundle",
      );
      toast.error(
        String(error).includes("MENU_MAKER_SOURCE_CHANGED")
          ? t("page.tools.menu_maker.source_changed")
          : t("page.tools.menu_maker.apply_failed"),
      );
    } finally {
      dispatch({ type: "busy", value: false });
    }
  };

  const saveINI = async () => {
    if (!state.source) return;
    const selection = await Dialog.SaveFile({
      suggestedName: outputName,
      filters: [{ name: "INI", extensions: ["ini"] }],
    });
    if (selection.canceled || !selection.filePath) return;
    dispatch({ type: "busy", value: true });
    try {
      await MenuMaker.SaveINI({
        destinationPath: selection.filePath,
        sourceText: state.source.text,
        slots: state.slots,
        settings: state.settings,
        encoding: state.source.encoding,
        hasBOM: state.source.hasBOM,
        newline: state.source.newline,
      });
      toast.success(t("page.tools.menu_maker.saved"));
    } catch (error) {
      Logger.error(
        { error, destinationPath: selection.filePath, outputName },
        "MenuMakerPage:saveINI",
      );
      toast.error(t("page.tools.menu_maker.save_failed"));
    } finally {
      dispatch({ type: "busy", value: false });
    }
  };

  const saveZIP = async () => {
    if (!state.source) return;
    const selection = await Dialog.SaveFile({
      suggestedName: outputName.replace(/\.ini$/i, ".zip"),
      filters: [{ name: "ZIP", extensions: ["zip"] }],
    });
    if (selection.canceled || !selection.filePath) return;
    dispatch({ type: "busy", value: true });
    try {
      const generated = await generateCurrent();
      if (!generated) return;
      const assets = await buildAssets(generated);
      if (!assets) return;
      await MenuMaker.SaveZIP({
        destinationPath: selection.filePath,
        outputININame: outputName,
        sourceText: state.source.text,
        slots: state.slots,
        settings: state.settings,
        encoding: state.source.encoding,
        hasBOM: state.source.hasBOM,
        newline: state.source.newline,
        assets,
      });
      toast.success(t("page.tools.menu_maker.saved"));
    } catch (error) {
      Logger.error(
        { error, destinationPath: selection.filePath, outputName },
        "MenuMakerPage:saveZIP",
      );
      toast.error(t("page.tools.menu_maker.save_failed"));
    } finally {
      dispatch({ type: "busy", value: false });
    }
  };

  const setSlot = (id: string, transform: (slot: MenuMakerSlot) => MenuMakerSlot) =>
    dispatch({
      type: "slots",
      value: state.slots.map((slot) => (slot.id === id ? transform(slot) : slot)),
    });

  const moveSlot = (from: string, to: string) =>
    dispatch({ type: "slots", value: moveMenuMakerSlot(state.slots, from, to) });

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
        <DraftDialog
          onRestore={async (draft) => {
            try {
              const blobs = await loadDraftBlobs(draft.id);
              const matchesCurrent = Boolean(
                state.source &&
                state.document &&
                canRestoreDraft(
                  draft,
                  state.source.sha256,
                  slotSignature(state.document.slots ?? []),
                ),
              );
              const originalText = matchesCurrent ? state.source?.text : blobs?.originalText;
              if (typeof originalText !== "string")
                throw new Error("MENU_MAKER_DRAFT_TEXT_MISSING");
              const document = matchesCurrent
                ? state.document!
                : await MenuMaker.Parse(originalText);
              if (!canRestoreDraft(draft, draft.sourceSHA256, slotSignature(document.slots ?? [])))
                throw new Error("MENU_MAKER_DRAFT_SIGNATURE_CHANGED");
              const restored = restoreDraftMedia(draft, blobs);
              dispatch({
                type: "load",
                source: matchesCurrent
                  ? state.source!
                  : {
                      path: draft.sourcePath,
                      fileName: draft.sourceName,
                      text: originalText,
                      sha256: draft.sourceSHA256,
                      encoding: draft.sourceEncoding ?? "utf8",
                      hasBOM: draft.sourceHasBOM ?? false,
                      newline: draft.sourceNewline ?? "lf",
                      document,
                    },
                document,
                slots: restored.slots,
                settings: restored.settings,
                sourceAvailable: matchesCurrent ? state.sourceAvailable : false,
              });
              setDialog(null);
              toast.success(t("page.tools.menu_maker.draft_restored"));
            } catch (error) {
              Logger.error({ error, draftId: draft.id }, "MenuMakerPage:restoreDraft");
              toast.error(t("page.tools.menu_maker.draft_restore_failed"));
            }
          }}
          onClose={() => setDialog(null)}
          t={t}
        />
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

function reducer(state: EditorState, action: EditorAction): EditorState {
  if (action.type === "busy") return { ...state, busy: action.value };
  if (action.type === "sourceAvailable") return { ...state, sourceAvailable: action.value };
  if (action.type === "sourceContent") {
    if (!state.source) return state;
    return {
      ...state,
      source: { ...state.source, text: action.text, sha256: action.sha256 },
    };
  }
  if (action.type === "scan") return { ...state, scan: action.value };
  if (action.type === "load")
    return {
      ...state,
      source: action.source,
      document: action.document,
      slots: action.slots ?? withSuggestedIcons(action.document.slots),
      settings: action.settings ?? state.settings,
      sourceAvailable: action.sourceAvailable ?? true,
      revision: state.revision + 1,
    };
  if (action.type === "slots")
    return { ...state, slots: action.value, revision: state.revision + 1 };
  if (action.type === "settings")
    return {
      ...state,
      settings: { ...state.settings, ...action.value },
      revision: state.revision + 1,
    };
  return {
    ...state,
    settings: {
      ...state.settings,
      palette: { ...state.settings.palette, [action.key]: action.value },
    },
    revision: state.revision + 1,
  };
}

function Inspector({
  settings,
  dispatch,
}: {
  settings: MenuMakerSettings;
  dispatch: React.Dispatch<EditorAction>;
}) {
  const { t } = useTranslation();
  const set = <K extends keyof MenuMakerSettings>(key: K, value: MenuMakerSettings[K]) =>
    dispatch({ type: "settings", value: { [key]: value } });
  return (
    <div className="space-y-5">
      <FieldSet className="gap-2">
        <FieldLegend className="mb-2 text-xs font-semibold text-muted-foreground uppercase">
          {t("page.tools.menu_maker.panel")}
        </FieldLegend>
        <SettingField label={t("page.tools.menu_maker.menu_title")}>
          <Input
            value={settings.title}
            placeholder={t("page.tools.menu_maker.menu_title_placeholder")}
            onChange={(event) => set("title", event.target.value)}
          />
        </SettingField>
        <div className="grid grid-cols-2 gap-2">
          <SettingField label={t("page.tools.menu_maker.menu_key")}>
            <Input
              value={settings.menuKey}
              onChange={(event) => set("menuKey", event.target.value)}
            />
          </SettingField>
          <SettingField label={t("page.tools.menu_maker.click_modifier")}>
            <SettingSelect
              value={settings.clickModifier}
              items={CLICK_MODIFIERS}
              onChange={(value) => set("clickModifier", value)}
              className="w-full"
            />
          </SettingField>
        </div>
        <div className="grid grid-cols-3 gap-2">
          <NumberField
            label={t("page.tools.menu_maker.columns")}
            value={settings.columns}
            min={1}
            max={8}
            onChange={(value) => set("columns", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.gap")}
            value={settings.gap}
            min={0}
            max={64}
            onChange={(value) => set("gap", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.scale")}
            value={settings.panelScale}
            min={0.5}
            max={2}
            step={0.1}
            onChange={(value) => set("panelScale", value)}
          />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <NumberField
            label={t("page.tools.menu_maker.base_width")}
            value={settings.baseWidth}
            min={640}
            max={7680}
            onChange={(value) => set("baseWidth", value)}
          />
          <NumberField
            label={t("page.tools.menu_maker.base_height")}
            value={settings.baseHeight}
            min={360}
            max={4320}
            onChange={(value) => set("baseHeight", value)}
          />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <SettingField label={t("page.tools.menu_maker.fallback")}>
            <SettingSelect
              value={settings.fallbackType}
              items={FALLBACK_TYPES}
              onChange={(value) => set("fallbackType", value)}
              className="w-full"
            />
          </SettingField>
          <SettingField label={t("page.tools.menu_maker.button_alignment")}>
            <SettingSelect
              value={settings.slotAlignment}
              items={SLOT_ALIGNMENTS}
              onChange={(value) => set("slotAlignment", value)}
              className="w-full"
            />
          </SettingField>
        </div>
        <Toggle
          label={t("page.tools.menu_maker.remove_keys")}
          value={settings.removeOriginalKeys}
          onChange={(value) => set("removeOriginalKeys", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.key_hint")}
          value={settings.showKeyHint}
          onChange={(value) => set("showKeyHint", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.hide_upload_label")}
          value={settings.hideUploadLabel}
          onChange={(value) => set("hideUploadLabel", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.original_name")}
          value={settings.useOriginalININame}
          onChange={(value) => set("useOriginalININame", value)}
        />
        <Toggle
          label={t("page.tools.menu_maker.active_reset")}
          value={settings.resetActiveOnPresent}
          onChange={(value) => set("resetActiveOnPresent", value)}
        />
      </FieldSet>
      <FieldSet className="gap-2">
        <FieldLegend className="mb-2 text-xs font-semibold text-muted-foreground uppercase">
          {t("page.tools.menu_maker.palette")}
        </FieldLegend>
        <div className="grid grid-cols-2 gap-2">
          {(
            [
              "accent",
              "panelBackground",
              "panelBorder",
              "slotBackground",
              "slotHover",
              "slotBorder",
              "title",
              "titleShadow",
            ] as const
          ).map((key) => (
            <SettingField key={key} label={t(`page.tools.menu_maker.palette_${key}`)}>
              <Input
                type="color"
                value={settings.palette[key]}
                onChange={(event) => dispatch({ type: "palette", key, value: event.target.value })}
              />
            </SettingField>
          ))}
        </div>
        <div className="mt-2 grid grid-cols-2 gap-2">
          {(
            [
              "panelBackgroundAlpha",
              "panelBorderAlpha",
              "slotBackgroundAlpha",
              "slotHoverAlpha",
              "slotBorderAlpha",
            ] as const
          ).map((key) => (
            <NumberField
              key={key}
              label={t(`page.tools.menu_maker.palette_${key}`)}
              value={settings.palette[key]}
              min={0}
              max={255}
              onChange={(value) => dispatch({ type: "palette", key, value })}
            />
          ))}
        </div>
      </FieldSet>
    </div>
  );
}

function PreviewSlot({
  slot,
  size,
  settings,
}: {
  slot: MenuMakerSlot;
  size: number;
  settings: MenuMakerSettings;
}) {
  return (
    <div
      className="flex min-w-0 flex-col items-center justify-center overflow-hidden rounded-sm border p-1"
      style={{
        width: size,
        height: size,
        color: slot.icon.color,
        textAlign: settings.slotAlignment,
        backgroundColor: alphaColor(
          settings.palette.slotBackground,
          settings.palette.slotBackgroundAlpha,
        ),
        borderColor: alphaColor(settings.palette.slotBorder, settings.palette.slotBorderAlpha),
      }}
    >
      {slot.icon.kind === "lucide" &&
      iconNames.includes(slot.icon.name as (typeof iconNames)[number]) ? (
        <DynamicIcon
          name={slot.icon.name as (typeof iconNames)[number]}
          style={{ width: size * 0.42, height: size * 0.42 }}
        />
      ) : slot.icon.kind === "upload" ? (
        <img src={slot.icon.dataUrl} className="h-[45%] w-[45%] rounded object-cover" />
      ) : slot.icon.kind === "iconify" ? (
        <span className="h-[45%] w-[45%]" dangerouslySetInnerHTML={{ __html: slot.icon.svg }} />
      ) : null}
      {!(slot.icon.kind === "upload" && settings.hideUploadLabel) && (
        <span
          className="mt-0.5 w-full truncate text-white"
          style={{ fontSize: Math.max(6, (size * 8) / MENU_MAKER_BASE_SLOT_SIZE) }}
        >
          {slot.name}
        </span>
      )}
      {settings.showKeyHint && (
        <span
          className="w-full truncate"
          style={{
            color: settings.palette.accent,
            fontSize: Math.max(5, (size * 7) / MENU_MAKER_BASE_SLOT_SIZE),
          }}
        >
          {slot.key}
        </span>
      )}
    </div>
  );
}

function SlotEditor({
  slot,
  selected,
  settings,
  onSelect,
  onChange,
  onMove,
  onIcon,
  onUpload,
}: {
  slot: MenuMakerSlot;
  selected: boolean;
  settings: MenuMakerSettings;
  onSelect: (value: boolean) => void;
  onChange: (transform: (slot: MenuMakerSlot) => MenuMakerSlot) => void;
  onMove: (draggedId: string) => void;
  onIcon: () => void;
  onUpload: () => void;
}) {
  const { t } = useTranslation();
  return (
    <article
      draggable
      onDragStart={(event) => event.dataTransfer.setData("text/menu-maker-slot", slot.id)}
      onDragOver={(event) => event.preventDefault()}
      onDrop={(event) => {
        event.preventDefault();
        const draggedId = event.dataTransfer.getData("text/menu-maker-slot");
        if (draggedId) onMove(draggedId);
      }}
      className={cn(
        "group relative min-w-0 rounded border p-2",
        slot.skip && "opacity-40 grayscale",
      )}
      style={{
        minHeight: 116,
        backgroundColor: alphaColor(
          settings.palette.slotBackground,
          settings.palette.slotBackgroundAlpha,
        ),
        borderColor: settings.palette.slotBorder,
        textAlign: settings.slotAlignment,
      }}
    >
      <div className="flex items-center justify-between">
        <Checkbox
          checked={selected}
          onCheckedChange={(checked) => onSelect(checked === true)}
          aria-label={t("page.tools.menu_maker.select_slot")}
        />
        <span className="truncate text-[10px] text-muted-foreground">
          {(slot.handlers ?? []).length} handler
        </span>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="text-muted-foreground"
          onClick={() => onChange((value) => ({ ...value, skip: !value.skip }))}
        >
          {slot.skip ? t("page.tools.menu_maker.restore") : t("page.tools.menu_maker.skip")}
        </Button>
      </div>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="mx-auto my-1 size-9"
        style={{ color: slot.icon.color }}
        onClick={onIcon}
      >
        {slot.icon.kind === "lucide" &&
        iconNames.includes(slot.icon.name as (typeof iconNames)[number]) ? (
          <DynamicIcon name={slot.icon.name as (typeof iconNames)[number]} />
        ) : slot.icon.kind === "upload" ? (
          <img src={slot.icon.dataUrl} className="size-8 rounded object-cover" />
        ) : slot.icon.kind === "iconify" ? (
          <span className="size-8" dangerouslySetInnerHTML={{ __html: slot.icon.svg }} />
        ) : (
          <ImageIcon />
        )}
      </Button>
      <Input
        className="h-7 text-center text-xs"
        value={slot.name}
        onChange={(event) => onChange((value) => ({ ...value, name: event.target.value }))}
      />
      <div className="mt-1 flex gap-1">
        <Input
          className="h-6 min-w-0 px-1 text-center font-mono text-[10px]"
          value={slot.key}
          onChange={(event) => {
            try {
              onChange((value) => updateSlotKey(value, event.target.value));
            } catch {
              toast.error(t("page.tools.menu_maker.multi_key_locked"));
            }
          }}
        />
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t("page.tools.menu_maker.upload_icon")}
                onClick={onUpload}
              >
                <UploadIcon className="size-3" />
              </Button>
            }
          />
          <TooltipContent>{t("page.tools.menu_maker.upload_icon")}</TooltipContent>
        </Tooltip>
      </div>
    </article>
  );
}

function EmptyState({
  scan,
  onLoad,
  onIncludeTXT,
  t,
}: {
  scan?: MenuMakerScanResult;
  onLoad: (path: string) => Promise<void>;
  onIncludeTXT: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <div className="mx-auto max-w-xl rounded-lg border border-dashed border-border bg-card p-6">
      <h2 className="mb-2 font-medium">{t("page.tools.menu_maker.empty_title")}</h2>
      {scan?.files?.length ? (
        <div className="space-y-2">
          {scan.files.map((file) => (
            <Button
              key={file.path}
              type="button"
              variant="outline"
              className="h-auto w-full justify-between px-3 py-3 text-left font-normal"
              onClick={() => void onLoad(file.path)}
            >
              <span className="truncate">{file.relativePath}</span>
              <span className="text-xs text-muted-foreground uppercase">{file.kind}</span>
            </Button>
          ))}
        </div>
      ) : (
        scan && (
          <Button className="mt-3" variant="outline" onClick={onIncludeTXT}>
            {t("page.tools.menu_maker.include_txt")}
          </Button>
        )
      )}
    </div>
  );
}

function IconPicker({
  currentToken,
  onClose,
  onPick,
  t,
}: {
  currentToken: string;
  onClose: () => void;
  onPick: (icon: MenuMakerSlot["icon"]) => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<IconSearchResult[]>(() => searchLucideIcons(""));
  const [online, setOnline] = useState(false);
  const [collectionPrefix, setCollectionPrefix] = useState("");
  const [cache, setCache] = useState<IconifyCacheStats>({ count: 0, bytes: 0, prefixes: [] });
  const [favorites, setFavorites] = useState(getFavoriteIconifyPrefixes);
  const token = useRef(currentToken);
  useEffect(() => {
    token.current = currentToken;
  }, [currentToken]);
  useEffect(() => {
    void Promise.all([searchCachedIconifyIcons(""), getIconifyCacheStats()]).then(
      ([cached, stats]) => {
        setResults([...searchLucideIcons(query), ...cached]);
        setCache(stats);
      },
    );
  }, []);
  const searchOnline = async () => {
    const requestToken = token.current;
    setOnline(true);
    try {
      const iconify = await searchIconifyIcons(query);
      if (acceptsIconResult(token.current, requestToken)) {
        setResults([...searchLucideIcons(query), ...iconify]);
        setCache(await getIconifyCacheStats());
      }
    } catch (error) {
      Logger.capture("menu-maker:icon-search", error);
      setResults([...searchLucideIcons(query), ...(await searchCachedIconifyIcons(query))]);
      toast.error(t("page.tools.menu_maker.icon_offline"));
    } finally {
      setOnline(false);
    }
  };
  return (
    <Modal title={t("page.tools.menu_maker.icon_picker")} onClose={onClose}>
      <div className="flex gap-2">
        <Input
          autoFocus
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            void searchCachedIconifyIcons(event.target.value).then((cached) =>
              setResults([...searchLucideIcons(event.target.value), ...cached]),
            );
          }}
          placeholder={t("page.tools.menu_maker.icon_search")}
        />
        <Button variant="outline" disabled={!query || online} onClick={() => void searchOnline()}>
          {online ? <Loader2Icon className="animate-spin" /> : <SearchIcon />}
          {t("page.tools.menu_maker.online_search")}
        </Button>
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {t("page.tools.menu_maker.online_policy")}
      </p>
      <div className="mt-2 flex gap-2">
        <Input
          className="h-8"
          value={collectionPrefix}
          onChange={(event) => setCollectionPrefix(event.target.value)}
          placeholder={t("page.tools.menu_maker.collection_prefix")}
        />
        <Button
          size="sm"
          variant="outline"
          disabled={!collectionPrefix || online}
          onClick={() => {
            setOnline(true);
            void downloadIconifyCollection(collectionPrefix)
              .then(async (count) => {
                setCache(await getIconifyCacheStats());
                setResults([
                  ...searchLucideIcons(query),
                  ...(await searchCachedIconifyIcons(query)),
                ]);
                toast.success(t("page.tools.menu_maker.collection_cached", { count }));
              })
              .catch(() => toast.error(t("page.tools.menu_maker.icon_offline")))
              .finally(() => setOnline(false));
          }}
        >
          {t("page.tools.menu_maker.download_collection")}
        </Button>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1 text-[11px] text-muted-foreground">
        <span>
          {t("page.tools.menu_maker.icon_cache", {
            count: cache.count,
            size: formatBytes(cache.bytes),
          })}
        </span>
        {cache.prefixes.map((prefix) => (
          <ButtonGroup key={prefix}>
            <Button
              type="button"
              size="xs"
              variant="outline"
              onClick={() => setFavorites(toggleFavoriteIconifyPrefix(prefix))}
            >
              <StarIcon className={favorites.includes(prefix) ? "fill-current" : undefined} />
              {prefix}
            </Button>
            <Button
              type="button"
              size="icon-xs"
              variant="outline"
              onClick={() =>
                void deleteIconifyPrefix(prefix).then(async () => {
                  setCache(await getIconifyCacheStats());
                  setResults([
                    ...searchLucideIcons(query),
                    ...(await searchCachedIconifyIcons(query)),
                  ]);
                })
              }
            >
              <XIcon />
            </Button>
          </ButtonGroup>
        ))}
        {cache.count > 0 && (
          <Button
            type="button"
            variant="link"
            size="xs"
            className="ml-auto h-auto px-0"
            onClick={() =>
              void clearIconifyCache().then(() => {
                setCache({ count: 0, bytes: 0, prefixes: [] });
                setResults(searchLucideIcons(query));
              })
            }
          >
            {t("page.tools.menu_maker.clear_cache")}
          </Button>
        )}
      </div>
      <div className="mt-3 grid max-h-[55vh] grid-cols-6 gap-2 overflow-auto">
        {results.map((result) => (
          <Button
            key={`${result.source}:${result.name}`}
            type="button"
            variant="outline"
            title={result.name}
            className="aspect-square size-auto"
            onClick={() =>
              result.source === "lucide"
                ? onPick({ kind: "lucide", name: result.name, color: "#ff4fb3" })
                : result.svg &&
                  sanitizeIconifySVG(result.svg) &&
                  onPick({
                    kind: "iconify",
                    name: result.name,
                    color: "#ff4fb3",
                    svg: sanitizeIconifySVG(result.svg)!,
                  })
            }
          >
            {result.source === "lucide" &&
            iconNames.includes(result.name as (typeof iconNames)[number]) ? (
              <DynamicIcon name={result.name as (typeof iconNames)[number]} />
            ) : result.svg ? (
              <span className="size-6" dangerouslySetInnerHTML={{ __html: result.svg }} />
            ) : null}
          </Button>
        ))}
      </div>
    </Modal>
  );
}

function CropDialog({
  source,
  size,
  onClose,
  onConfirm,
  t,
}: {
  source: string;
  size: number;
  onClose: () => void;
  onConfirm: (dataUrl: string) => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [zoom, setZoom] = useState(1);
  const [image, setImage] = useState<HTMLImageElement>();
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | undefined>(undefined);
  const previewRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const next = new Image();
    next.src = source;
    let cancelled = false;
    void next.decode().then(
      () => {
        if (!cancelled) setImage(next);
      },
      (error: unknown) => {
        if (!cancelled) Logger.error({ error }, "MenuMakerPage:cropDecode");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [source]);

  useEffect(() => {
    const canvas = previewRef.current;
    const context = canvas?.getContext("2d");
    if (!canvas || !context || !image?.naturalWidth) return;
    drawCoveredImage(
      context,
      image,
      image.naturalWidth,
      image.naturalHeight,
      MENU_MAKER_CROP_PREVIEW_SIZE,
      zoom,
      offset,
    );
  }, [image, offset, zoom]);

  const confirm = () => {
    if (!image?.naturalWidth) return;
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const context = canvas.getContext("2d");
    if (!context) return;
    drawCoveredImage(context, image, image.naturalWidth, image.naturalHeight, size, zoom, offset);
    onConfirm(canvas.toDataURL("image/png"));
  };

  return (
    <Modal title={t("page.tools.menu_maker.crop")} onClose={onClose}>
      <canvas
        ref={previewRef}
        width={MENU_MAKER_CROP_PREVIEW_SIZE}
        height={MENU_MAKER_CROP_PREVIEW_SIZE}
        className="mx-auto size-64 touch-none rounded border border-border bg-black"
        onWheel={(event) => {
          event.preventDefault();
          setZoom((value) => Math.max(1, Math.min(4, value - event.deltaY * 0.001)));
        }}
        onPointerDown={(event) => {
          drag.current = { x: event.clientX, y: event.clientY, ox: offset.x, oy: offset.y };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          if (!drag.current) return;
          const bounds = event.currentTarget.getBoundingClientRect();
          setOffset({
            x:
              drag.current.ox +
              ((event.clientX - drag.current.x) * event.currentTarget.width) / bounds.width,
            y:
              drag.current.oy +
              ((event.clientY - drag.current.y) * event.currentTarget.height) / bounds.height,
          });
        }}
        onPointerUp={() => {
          drag.current = undefined;
        }}
      />
      <p className="mt-2 text-center text-xs text-muted-foreground">
        {t("page.tools.menu_maker.crop_hint")}
      </p>
      <DialogFooter className="mt-4">
        <Button variant="outline" onClick={onClose}>
          {t("g.cancel")}
        </Button>
        <Button disabled={!image?.naturalWidth} onClick={confirm}>
          {t("g.confirm")}
        </Button>
      </DialogFooter>
    </Modal>
  );
}

function DraftDialog({
  onRestore,
  onClose,
  t,
}: {
  onRestore: (draft: ReturnType<typeof loadDraftMetadata>[number]) => Promise<void>;
  onClose: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [drafts, setDrafts] = useState(loadDraftMetadata);
  return (
    <Modal title={t("page.tools.menu_maker.drafts")} onClose={onClose}>
      <div className="max-h-[60vh] space-y-2 overflow-auto">
        {drafts.map((draft) => (
          <div key={draft.id} className="flex items-center gap-2 rounded border border-border p-3">
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">{draft.sourceName}</p>
              <p className="text-xs text-muted-foreground">
                {new Date(draft.updatedAt).toLocaleString()}
              </p>
            </div>
            <Button size="sm" onClick={() => void onRestore(draft)}>
              {t("page.tools.menu_maker.restore")}
            </Button>
            <Button
              size="icon"
              variant="ghost"
              onClick={() => {
                void deleteDraftBlobs(draft.id).catch((error) =>
                  Logger.error({ error, draftId: draft.id }, "MenuMakerPage:deleteDraftBlobs"),
                );
                setDrafts(saveDraftMetadata(drafts.filter((item) => item.id !== draft.id)));
              }}
            >
              <XIcon />
            </Button>
          </div>
        ))}
      </div>
      {drafts.length > 0 && (
        <Button
          className="mt-3"
          variant="destructive"
          onClick={() => {
            void Promise.all(drafts.map((draft) => deleteDraftBlobs(draft.id))).catch((error) =>
              Logger.error({ error }, "MenuMakerPage:clearDraftBlobs"),
            );
            setDrafts(saveDraftMetadata([]));
          }}
        >
          {t("page.tools.menu_maker.delete_all")}
        </Button>
      )}
    </Modal>
  );
}

function CompareDialog({
  original,
  generated,
  onClose,
  t,
}: {
  original: string;
  generated: string;
  onClose: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <Modal title={t("page.tools.menu_maker.compare")} onClose={onClose} wide>
      <div className="grid max-h-[70vh] grid-cols-1 gap-3 md:grid-cols-2">
        <div>
          <h3 className="mb-1 text-xs font-medium">{t("page.tools.menu_maker.original_logic")}</h3>
          <pre className="h-[60vh] overflow-auto rounded bg-muted p-3 text-[11px] select-text">
            {original}
          </pre>
        </div>
        <div>
          <h3 className="mb-1 text-xs font-medium">{t("page.tools.menu_maker.generated_logic")}</h3>
          <pre className="h-[60vh] overflow-auto rounded bg-muted p-3 text-[11px] select-text">
            {generated}
          </pre>
        </div>
      </div>
    </Modal>
  );
}

function Modal({
  title,
  onClose,
  wide,
  children,
}: {
  title: string;
  onClose: () => void;
  wide?: boolean;
  children: React.ReactNode;
}) {
  return (
    <ModalDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className={cn("sm:max-w-2xl", wide && "max-w-[calc(100%-2rem)] sm:max-w-6xl")}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {children}
      </DialogContent>
    </ModalDialog>
  );
}
const CLICK_MODIFIERS = [
  { value: "alt", label: "alt" },
  { value: "ctrl", label: "ctrl" },
  { value: "shift", label: "shift" },
  { value: "none", label: "none" },
] as const satisfies readonly { value: MenuMakerSettings["clickModifier"]; label: string }[];

const FALLBACK_TYPES = [
  { value: "cycle", label: "cycle" },
  { value: "toggle", label: "toggle" },
  { value: "hold", label: "hold" },
  { value: "activate", label: "activate" },
] as const satisfies readonly { value: MenuMakerSettings["fallbackType"]; label: string }[];

const SLOT_ALIGNMENTS = [
  { value: "left", label: "left" },
  { value: "center", label: "center" },
  { value: "right", label: "right" },
] as const satisfies readonly { value: MenuMakerSettings["slotAlignment"]; label: string }[];

function SettingSelect<T extends string>({
  value,
  items,
  onChange,
  disabled,
  className,
  size,
}: {
  value: T;
  items: readonly { value: T; label: string }[];
  onChange: (value: T) => void;
  disabled?: boolean;
  className?: string;
  size?: "sm" | "default";
}) {
  return (
    <Select
      value={value}
      items={[...items]}
      onValueChange={(next) => {
        if (next === null) return;
        onChange(next);
      }}
    >
      <SelectTrigger size={size} disabled={disabled} className={className}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function SettingField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Field className="gap-1">
      <FieldLabel className="text-xs font-normal text-muted-foreground">{label}</FieldLabel>
      {children}
    </Field>
  );
}
function NumberField({
  label,
  value,
  min,
  max,
  step = 1,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  onChange: (value: number) => void;
}) {
  return (
    <SettingField label={label}>
      <Input
        type="number"
        value={value}
        min={min}
        max={max}
        step={step}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </SettingField>
  );
}
function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <Field orientation="horizontal" className="items-center justify-between gap-3 py-1">
      <FieldLabel className="text-xs font-normal">{label}</FieldLabel>
      <Switch checked={value} onCheckedChange={onChange} />
    </Field>
  );
}
function buildOutputName(fileName: string, original: boolean): string {
  const base = fileName.replace(/\.(?:ini|txt)$/i, "");
  return `${base}${original ? "" : "_gui"}.ini`;
}
function alphaColor(hex: string, alpha: number): string {
  return `${hex}${Math.round(Math.max(0, Math.min(255, alpha)))
    .toString(16)
    .padStart(2, "0")}`;
}

function formatBytes(bytes: number): string {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`;
}

function loadStoredSettings(): MenuMakerSettings {
  if (typeof localStorage === "undefined") return DEFAULT_MENU_MAKER_SETTINGS;
  try {
    const value: unknown = JSON.parse(localStorage.getItem("nahida.menu-maker.settings") ?? "null");
    if (!value || typeof value !== "object") return DEFAULT_MENU_MAKER_SETTINGS;
    const stored = value as Partial<MenuMakerSettings>;
    return {
      ...DEFAULT_MENU_MAKER_SETTINGS,
      ...stored,
      columns: stored.columns || DEFAULT_MENU_MAKER_SETTINGS.columns,
      palette: { ...DEFAULT_MENU_MAKER_SETTINGS.palette, ...stored.palette },
    };
  } catch (error) {
    Logger.capture("menu-maker:read-settings", error);
    return DEFAULT_MENU_MAKER_SETTINGS;
  }
}
