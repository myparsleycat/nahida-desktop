import type { Emitter, Inspection, Look } from "@bindings/tools/liquid_emitter";
import type { ModelViewerSurfacePick } from "@renderer/components/tools/model-viewer/model-viewer-contract";
import {
  ALL_FORMS,
  ModelViewerWorkspace,
} from "@renderer/components/tools/model-viewer/model-viewer-workspace";
import { RangeField } from "@renderer/components/tools/range-field";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { ButtonGroup } from "@renderer/components/ui/button-group";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { Input } from "@renderer/components/ui/input";
import { cn } from "@renderer/lib/utils";
import type { Vec3 } from "@shared/liquid-emitter/flow-trajectory";
import type { ViewerForm, ViewerMeshTransport } from "@shared/mod-viewer/types";
import { toErrorMessage } from "@shared/utils";
import { isEqual } from "es-toolkit";
import type { TFunction } from "i18next";
import {
  CrosshairIcon,
  Loader2Icon,
  Redo2Icon,
  Trash2Icon,
  TriangleAlertIcon,
  Undo2Icon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { LiquidEmitterOverlay } from "./liquid-emitter-overlay";
import {
  type ApplyMode,
  IN_PLACE,
  NEW_MOD,
  useLiquidEmitterSession,
} from "./use-liquid-emitter-session";

type Anchor = {
  meshId: string;
  faceIndex: number;
  vertices: Vec3;
  triangle: [Vec3, Vec3, Vec3];
  barycentric: Vec3;
  point: Vec3;
  offset: Vec3;
  direction: Vec3;
};

type History = {
  entries: Array<Anchor | null>;
  index: number;
  /** Consecutive edits with the same key, such as one slider drag, share one undo step. */
  lastKey?: string;
};

const HISTORY_LIMIT = 100;
// The runtime rejects a whole profile file when an offset axis exceeds 25 cm.
const MAX_OFFSET_METERS = 0.25;
const AXES = ["X", "Y", "Z"] as const;
const DOWN: Vec3 = [0, 0, -1];
// The runtime's strength levels run from 0 to 7.
const MAX_STRENGTH_OFFSET = 7;
const DEFAULT_ABSORPTION_MM = 15;
// Applying repairs an outdated or changed runtime, so only these leave editing open.
const EDITABLE_RUNTIME_STATES = new Set<string>(["current", "outdated", "modified"]);

export function LiquidEmitterTool({
  modPath,
  onModPathChange,
}: {
  modPath: string;
  /** Called when the tool moves to another folder: a new mod it created, or the restored original. */
  onModPathChange?: (modPath: string) => void;
}) {
  const { t } = useTranslation();
  const session = useLiquidEmitterSession(modPath);
  const [mode, setMode] = useState<ApplyMode>(IN_PLACE);
  const [confirmRestore, setConfirmRestore] = useState(false);
  const [history, setHistory] = useState<History>({ entries: [null], index: 0 });
  const [seeded, setSeeded] = useState(false);
  const [picking, setPicking] = useState(true);
  const [showTrajectory, setShowTrajectory] = useState(false);
  const [doubleSided, setDoubleSided] = useState(true);
  const [confirmRemoveAll, setConfirmRemoveAll] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [form, setForm] = useState(ALL_FORMS);

  const inspection = session.inspection.data;
  const emitters = inspection?.emitters ?? [];
  // Anchors are edited in the mesh's own units, which are not meters in every game.
  const meshUnit = inspection?.meshUnit || 1;
  const meshes = session.source?.transport.meshes;
  const forms = session.source?.transport.forms ?? [];
  const formOfMesh = (meshId: string) => meshes?.find((mesh) => mesh.id === meshId)?.form;
  const anchor = history.entries[history.index];
  const current = anchor
    ? emitters.find((emitter) => emitter.anchor.meshId === anchor.meshId)
    : undefined;
  const busy =
    session.apply.isPending ||
    session.remove.isPending ||
    session.install.isPending ||
    session.setLook.isPending ||
    session.restore.isPending;
  const editable =
    inspection !== undefined && EDITABLE_RUNTIME_STATES.has(inspection.runtime.state);
  const locked = !editable || !anchor;
  const picks = picking && editable;
  // A mod this tool generated is always edited in place.
  const applyMode = inspection?.sourcePath ? IN_PLACE : mode;

  // Start from the first saved emitter whose mesh the viewer still has.
  if (!seeded && inspection && meshes) {
    setSeeded(true);
    const saved = emitters
      .map(anchorFromEmitter)
      .find((candidate) => candidate && meshes.some((mesh) => mesh.id === candidate.meshId));
    if (saved) setHistory({ entries: [saved], index: 0 });
    // A bundle of forms opens on the form being edited, or its first, so the
    // forms drawn over one another do not hide each other's surfaces.
    setForm(
      (saved && meshes.find((mesh) => mesh.id === saved.meshId)?.form) ?? forms[0]?.id ?? ALL_FORMS,
    );
  }

  const commit = (next: Anchor | null, key?: string) =>
    setHistory((state) => {
      if (key && key === state.lastKey && state.index === state.entries.length - 1) {
        const entries = [...state.entries];
        entries[state.index] = next;
        return { ...state, entries };
      }
      const entries = [...state.entries.slice(0, state.index + 1), next].slice(-HISTORY_LIMIT);
      return { entries, index: entries.length - 1, lastKey: key };
    });
  const step = (delta: number) =>
    setHistory((state) => ({
      ...state,
      index: Math.min(Math.max(state.index + delta, 0), state.entries.length - 1),
      lastKey: undefined,
    }));
  const update = (key: string, change: Partial<Anchor>) => {
    if (anchor) commit({ ...anchor, ...change }, key);
  };

  const handlePick = (pick: ModelViewerSurfacePick | null) => {
    if (!pick) {
      setNotice(t("page.tools.liquid_emitter.pick.missed"));
      return;
    }
    setNotice(null);
    commit({
      meshId: pick.meshId,
      faceIndex: pick.faceIndex,
      vertices: pick.vertices,
      triangle: pick.triangle,
      barycentric: pick.barycentric,
      point: pick.point,
      offset: [0, 0, 0],
      direction: anchor?.direction ?? DOWN,
    });
  };

  const setOffsetAxis = (axis: number, meters: number, key: string) => {
    if (!anchor) return;
    const offset = [...anchor.offset] as Vec3;
    offset[axis] = meters / meshUnit;
    update(key, { offset: limitOffset(offset, MAX_OFFSET_METERS / meshUnit) });
  };

  const setDirectionAxis = (axis: number, value: number) => {
    if (!anchor || !Number.isFinite(value)) return;
    const direction = [...anchor.direction] as Vec3;
    direction[axis] = value;
    if (Math.hypot(...direction) > 0.001) update(`direction-${axis}`, { direction });
  };

  const handleApply = () => {
    if (!anchor || !session.source) return;
    session.apply.mutate(
      {
        modPath,
        viewerSessionId: session.source.memorySessionId,
        meshId: anchor.meshId,
        faceIndex: anchor.faceIndex,
        faceVertices: anchor.vertices,
        barycentric: anchor.barycentric,
        trianglePositions: anchor.triangle.flat(),
        point: anchor.point,
        offset: anchor.offset,
        direction: anchor.direction,
        mode: applyMode,
      },
      {
        onSuccess: (result) => {
          if (applyMode !== NEW_MOD) {
            toast.success(t("page.tools.liquid_emitter.applied"));
            return;
          }
          toast.success(t("page.tools.liquid_emitter.created"), { description: result.modPath });
          onModPathChange?.(result.modPath);
        },
        onError: (error) => toast.error(liquidErrorMessage(t, error)),
      },
    );
  };

  const handleRestore = () =>
    session.restore.mutate(undefined, {
      onSuccess: (result) => {
        toast.success(t("page.tools.liquid_emitter.restored"), { description: result.modPath });
        onModPathChange?.(result.modPath);
      },
      onError: (error) => toast.error(liquidErrorMessage(t, error)),
    });

  const handleRemove = (pids: number[]) =>
    session.remove.mutate(pids, {
      onSuccess: () => toast.success(t("page.tools.liquid_emitter.removed")),
      onError: (error) => toast.error(liquidErrorMessage(t, error)),
    });

  const handleSaveLook = (look: Look) =>
    session.setLook.mutateAsync(look).then(
      () => {
        toast.success(t("page.tools.liquid_emitter.look.saved"));
        return true;
      },
      (error: unknown) => {
        toast.error(liquidErrorMessage(t, error));
        return false;
      },
    );

  const handleInstall = () =>
    session.install.mutate(undefined, {
      onSuccess: () => toast.success(t("page.tools.liquid_emitter.runtime.installed")),
      onError: (error) => toast.error(liquidErrorMessage(t, error)),
    });

  if (session.loadError) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>{t("page.tools.liquid_emitter.load_error")}</AlertTitle>
        <AlertDescription>{session.loadError}</AlertDescription>
      </Alert>
    );
  }

  if (!session.source) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2Icon className="size-5 animate-spin" aria-hidden="true" />
        {t("page.tools.model_viewer.loading")}
      </div>
    );
  }

  const meshLabel = anchor
    ? (meshes?.find((mesh) => mesh.id === anchor.meshId)?.component ?? anchor.meshId)
    : "";
  const anchorForm = anchor ? formOfMesh(anchor.meshId) : undefined;
  const anchorShown = anchor !== null && (form === ALL_FORMS || !anchorForm || anchorForm === form);
  const position = anchor
    ? (anchor.point.map((value, axis) => value + anchor.offset[axis]) as Vec3)
    : undefined;

  const inspector = (
    <div className="rounded-md bg-muted/30">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
        <div className="text-sm font-medium">{t("page.tools.liquid_emitter.title")}</div>
        <Button
          size="sm"
          variant={picks ? "default" : "outline"}
          aria-pressed={picks}
          disabled={!editable}
          onClick={() => setPicking((value) => !value)}
        >
          <CrosshairIcon data-icon="inline-start" />
          {t("page.tools.liquid_emitter.pick.toggle")}
        </Button>
      </div>

      <div className="space-y-5 p-4">
        <RuntimeSection
          inspection={inspection}
          error={session.inspection.error}
          busy={busy}
          installing={session.install.isPending}
          onInstall={handleInstall}
        />

        <section className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-sm font-medium">
              {t("page.tools.liquid_emitter.selection.title")}
            </h3>
            <div className="flex gap-1">
              <Button
                size="icon-sm"
                variant="ghost"
                disabled={!editable || history.index === 0}
                aria-label={t("page.tools.liquid_emitter.history.undo")}
                title={t("page.tools.liquid_emitter.history.undo")}
                onClick={() => step(-1)}
              >
                <Undo2Icon />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                disabled={!editable || history.index >= history.entries.length - 1}
                aria-label={t("page.tools.liquid_emitter.history.redo")}
                title={t("page.tools.liquid_emitter.history.redo")}
                onClick={() => step(1)}
              >
                <Redo2Icon />
              </Button>
            </div>
          </div>
          <div className="rounded-md border bg-background/40 px-3 py-2 text-sm">
            {anchor ? (
              <>
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-medium" title={meshLabel}>
                    {meshLabel}
                  </span>
                  <FormBadge forms={forms} form={anchorForm} />
                </div>
                <div className="text-xs text-muted-foreground">
                  {t("page.tools.liquid_emitter.selection.triangle", { index: anchor.faceIndex })}
                </div>
              </>
            ) : (
              <span className="text-muted-foreground">
                {t("page.tools.liquid_emitter.selection.empty")}
              </span>
            )}
          </div>
          {notice ? <p className="text-xs text-destructive">{notice}</p> : null}
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.position.title")}</h3>
          <div className="grid grid-cols-3 gap-2">
            {AXES.map((label, axis) => (
              <label key={label} className="space-y-1 text-xs text-muted-foreground">
                {label}
                <Input
                  type="number"
                  step={0.001}
                  disabled={locked}
                  value={position ? Number(position[axis].toFixed(4)) : ""}
                  onChange={(event) => {
                    const value = Number(event.currentTarget.value);
                    if (anchor && Number.isFinite(value)) {
                      setOffsetAxis(
                        axis,
                        (value - anchor.point[axis]) * meshUnit,
                        `position-${axis}`,
                      );
                    }
                  }}
                />
              </label>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            {t("page.tools.liquid_emitter.position.hint")}
          </p>
        </section>

        <section className="space-y-3">
          <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.offset.title")}</h3>
          {AXES.map((label, axis) => {
            const millimeters = (anchor?.offset[axis] ?? 0) * meshUnit * 1000;
            return (
              <RangeField
                key={label}
                label={label}
                value={millimeters}
                defaultValue={0}
                min={-150}
                max={150}
                step={0.5}
                display={`${millimeters.toFixed(1)} mm`}
                disabled={locked}
                onChange={(value) => setOffsetAxis(axis, value / 1000, `offset-${axis}`)}
              />
            );
          })}
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={locked}
            onClick={() => update("offset-reset", { offset: [0, 0, 0] })}
          >
            {t("page.tools.liquid_emitter.offset.reset")}
          </Button>
        </section>

        <section className="space-y-2">
          <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.direction.title")}</h3>
          <div className="grid grid-cols-3 gap-2">
            {AXES.map((label, axis) => (
              <label key={label} className="space-y-1 text-xs text-muted-foreground">
                {label}
                <Input
                  type="number"
                  step={0.1}
                  disabled={locked}
                  value={anchor ? Number(anchor.direction[axis].toFixed(3)) : ""}
                  onChange={(event) => setDirectionAxis(axis, Number(event.currentTarget.value))}
                />
              </label>
            ))}
          </div>
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={locked}
            onClick={() => update("direction-down", { direction: DOWN })}
          >
            {t("page.tools.liquid_emitter.direction.down")}
          </Button>
          <label className="flex items-start gap-2 pt-1 text-sm">
            <Checkbox
              checked={showTrajectory}
              onCheckedChange={(checked) => setShowTrajectory(checked === true)}
            />
            <span>
              {t("page.tools.liquid_emitter.trajectory.label")}
              <span className="mt-0.5 block text-xs text-muted-foreground">
                {t("page.tools.liquid_emitter.trajectory.hint")}
              </span>
            </span>
          </label>
        </section>

        <ApplyModeSection
          inspection={inspection}
          mode={mode}
          busy={busy}
          editable={editable}
          onModeChange={setMode}
          onRestore={() => setConfirmRestore(true)}
        />

        <section className="space-y-2">
          <Button
            className="w-full"
            disabled={locked || busy}
            isLoading={session.apply.isPending}
            onClick={handleApply}
          >
            {t(
              applyMode === NEW_MOD
                ? "page.tools.liquid_emitter.mode.new_mod"
                : "page.tools.liquid_emitter.apply",
            )}
          </Button>
          {current ? (
            <Button
              variant="outline"
              className="w-full"
              disabled={busy}
              onClick={() => handleRemove([current.pid])}
            >
              {t("page.tools.liquid_emitter.remove")}
            </Button>
          ) : null}
          <p className="text-xs text-muted-foreground">{t("page.tools.liquid_emitter.f10_hint")}</p>
        </section>

        <EmitterList
          emitters={emitters}
          meshes={meshes}
          forms={forms}
          selectedMeshId={anchor?.meshId}
          busy={busy}
          onSelect={(emitter) => {
            const saved = anchorFromEmitter(emitter);
            if (!saved) return;
            commit(saved);
            const savedForm = formOfMesh(saved.meshId);
            if (savedForm && form !== ALL_FORMS) setForm(savedForm);
          }}
          onRemove={(emitter) => handleRemove([emitter.pid])}
          onRemoveAll={() => setConfirmRemoveAll(true)}
        />

        <LookSection
          key={inspection?.modPath}
          inspection={inspection}
          busy={busy}
          editable={editable}
          onSave={handleSaveLook}
        />
      </div>
    </div>
  );

  return (
    <>
      <ModelViewerWorkspace
        open
        source={session.source}
        doubleSidedEnabled={doubleSided}
        onDoubleSidedChange={setDoubleSided}
        onSurfacePick={picks ? handlePick : undefined}
        form={form}
        onFormChange={setForm}
        overlay={
          anchor && anchorShown ? (
            <LiquidEmitterOverlay
              point={anchor.point}
              offset={anchor.offset}
              direction={anchor.direction}
              meshUnit={meshUnit}
              showTrajectory={showTrajectory}
            />
          ) : null
        }
        inspector={inspector}
      />

      <AlertDialog open={confirmRemoveAll} onOpenChange={setConfirmRemoveAll}>
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.tools.liquid_emitter.remove_all.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.liquid_emitter.remove_all.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmRemoveAll(false);
                handleRemove([]);
              }}
            >
              {t("page.tools.liquid_emitter.remove_all.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmRestore} onOpenChange={setConfirmRestore}>
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.tools.liquid_emitter.generated.restore_title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.liquid_emitter.generated.restore_description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmRestore(false);
                handleRestore();
              }}
            >
              {t("page.tools.liquid_emitter.generated.restore_confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function ApplyModeSection({
  inspection,
  mode,
  busy,
  editable,
  onModeChange,
  onRestore,
}: {
  inspection: Inspection | undefined;
  mode: ApplyMode;
  busy: boolean;
  editable: boolean;
  onModeChange: (mode: ApplyMode) => void;
  onRestore: () => void;
}) {
  const { t } = useTranslation();
  if (!inspection) return null;

  if (inspection.sourcePath) {
    return (
      <section className="space-y-2 rounded-md border bg-background/40 px-3 py-2">
        <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.generated.title")}</h3>
        <p className="text-xs break-all text-muted-foreground">
          {t("page.tools.liquid_emitter.generated.description", { path: inspection.sourcePath })}
        </p>
        <Button size="sm" variant="outline" className="w-full" disabled={busy} onClick={onRestore}>
          {t("page.tools.liquid_emitter.generated.restore")}
        </Button>
      </section>
    );
  }

  return (
    <section className="space-y-2">
      <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.mode.title")}</h3>
      <ButtonGroup className="w-full">
        {([IN_PLACE, NEW_MOD] as const).map((value) => (
          <Button
            key={value}
            size="sm"
            className="flex-1"
            variant={mode === value ? "default" : "outline"}
            aria-pressed={mode === value}
            disabled={busy || !editable}
            onClick={() => onModeChange(value)}
          >
            {t(`page.tools.liquid_emitter.mode.${value}`)}
          </Button>
        ))}
      </ButtonGroup>
      <p className="text-xs text-muted-foreground">
        {t(`page.tools.liquid_emitter.mode.${mode}_hint`)}
      </p>
    </section>
  );
}

function RuntimeSection({
  inspection,
  error,
  busy,
  installing,
  onInstall,
}: {
  inspection: Inspection | undefined;
  error: unknown;
  busy: boolean;
  installing: boolean;
  onInstall: () => void;
}) {
  const { t } = useTranslation();
  if (error) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertDescription>{liquidErrorMessage(t, error)}</AlertDescription>
      </Alert>
    );
  }
  if (!inspection) {
    return <Loader2Icon className="size-4 animate-spin text-muted-foreground" aria-hidden="true" />;
  }

  const state = inspection.runtime.state;
  if (state === "foreign") {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>{t("page.tools.liquid_emitter.runtime.foreign_title")}</AlertTitle>
        <AlertDescription>
          {t("page.tools.liquid_emitter.runtime.foreign_description", {
            path: inspection.runtime.path,
          })}
        </AlertDescription>
      </Alert>
    );
  }
  if (state === "missing" || state === "disabled") {
    return (
      <Alert>
        <TriangleAlertIcon />
        <AlertTitle>{t(`page.tools.liquid_emitter.runtime.required.${state}.title`)}</AlertTitle>
        <AlertDescription>
          {t(`page.tools.liquid_emitter.runtime.required.${state}.description`)}
        </AlertDescription>
        <div className="col-start-2 pt-2">
          <Button size="sm" disabled={busy} isLoading={installing} onClick={onInstall}>
            {t(`page.tools.liquid_emitter.runtime.action.${state}`)}
          </Button>
        </div>
      </Alert>
    );
  }

  return (
    <section className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2 text-sm">
        <span className="text-muted-foreground">
          {t("page.tools.liquid_emitter.runtime.title")}
        </span>
        <Badge variant={state === "current" ? "secondary" : "destructive"}>
          {t(`page.tools.liquid_emitter.runtime.state.${state}`)}
        </Badge>
      </div>
      {state !== "current" ? (
        <Button size="sm" variant="outline" disabled={busy} onClick={onInstall}>
          {t(`page.tools.liquid_emitter.runtime.action.${state}`)}
        </Button>
      ) : null}
    </section>
  );
}

function EmitterList({
  emitters,
  meshes,
  forms,
  selectedMeshId,
  busy,
  onSelect,
  onRemove,
  onRemoveAll,
}: {
  emitters: Emitter[];
  meshes: ViewerMeshTransport[] | undefined;
  forms: ViewerForm[];
  selectedMeshId: string | undefined;
  busy: boolean;
  onSelect: (emitter: Emitter) => void;
  onRemove: (emitter: Emitter) => void;
  onRemoveAll: () => void;
}) {
  const { t } = useTranslation();
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">
          {t("page.tools.liquid_emitter.emitters.title", { count: emitters.length })}
        </h3>
        {emitters.length > 0 ? (
          <Button size="xs" variant="ghost" disabled={busy} onClick={onRemoveAll}>
            {t("page.tools.liquid_emitter.remove_all.button")}
          </Button>
        ) : null}
      </div>
      {emitters.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {t("page.tools.liquid_emitter.emitters.empty")}
        </p>
      ) : (
        <ul className="space-y-1">
          {emitters.map((emitter) => {
            const mesh = meshes?.find((candidate) => candidate.id === emitter.anchor.meshId);
            return (
              <li
                key={emitter.pid}
                className={cn(
                  "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted/60",
                  emitter.anchor.meshId === selectedMeshId && "bg-muted",
                )}
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left disabled:cursor-not-allowed"
                  disabled={!mesh}
                  title={mesh ? undefined : t("page.tools.liquid_emitter.emitters.missing_mesh")}
                  onClick={() => onSelect(emitter)}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate">{mesh?.component ?? emitter.section}</span>
                    <FormBadge forms={forms} form={mesh?.form ?? iniFolder(emitter.iniPath)} />
                  </span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {emitter.iniPath} · #{emitter.pid}
                  </span>
                </button>
                {!emitter.hooked ? (
                  <Badge variant="destructive">
                    {t("page.tools.liquid_emitter.emitters.unhooked")}
                  </Badge>
                ) : null}
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy}
                  aria-label={t("page.tools.liquid_emitter.remove")}
                  title={t("page.tools.liquid_emitter.remove")}
                  onClick={() => onRemove(emitter)}
                >
                  <Trash2Icon />
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** Names the form of a bundled mod an emitter belongs to; nothing for a single mod. */
function FormBadge({ forms, form }: { forms: ViewerForm[]; form: string | undefined }) {
  const label = forms.find((entry) => entry.id === form)?.label;
  if (forms.length < 2 || !label) return null;
  return (
    <Badge variant="secondary" className="max-w-32 shrink-0 font-normal" title={label}>
      <span className="truncate">{label}</span>
    </Badge>
  );
}

// The folder of a mod-relative INI path, as the viewer names forms.
function iniFolder(iniPath: string) {
  const slash = iniPath.lastIndexOf("/");
  return slash < 0 ? "." : iniPath.slice(0, slash);
}

function LookSection({
  inspection,
  busy,
  editable,
  onSave,
}: {
  inspection: Inspection | undefined;
  busy: boolean;
  editable: boolean;
  onSave: (look: Look) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<Look | null>(null);
  if (!inspection) return null;

  const look = draft ?? inspection.look;
  const hasEmitters = (inspection.emitters ?? []).length > 0;
  const locked = !editable || !hasEmitters || busy;
  const dirty = draft !== null && !isEqual(draft, inspection.look);
  const update = (change: Partial<Look>) => setDraft({ ...look, ...change });
  const offset = look.strengthOffset;

  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.look.title")}</h3>
        <p className="text-xs text-muted-foreground">
          {t(
            hasEmitters
              ? "page.tools.liquid_emitter.look.hint"
              : "page.tools.liquid_emitter.look.needs_emitter",
          )}
        </p>
      </div>
      <div className="space-y-1">
        <RangeField
          label={t("page.tools.liquid_emitter.look.strength")}
          value={offset}
          defaultValue={0}
          min={-MAX_STRENGTH_OFFSET}
          max={MAX_STRENGTH_OFFSET}
          step={1}
          display={offset > 0 ? `+${offset}` : String(offset)}
          disabled={locked}
          onChange={(value) => update({ strengthOffset: value })}
        />
        <p className="text-xs text-muted-foreground">
          {t("page.tools.liquid_emitter.look.strength_hint")}
        </p>
      </div>
      <label className="flex items-start gap-2 text-sm">
        <Checkbox
          checked={look.customColor}
          disabled={locked}
          onCheckedChange={(checked) => update({ customColor: checked === true })}
        />
        <span>
          {t("page.tools.liquid_emitter.look.custom_color")}
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {t("page.tools.liquid_emitter.look.custom_color_hint")}
          </span>
        </span>
      </label>
      {look.customColor ? (
        <>
          <label className="flex items-center justify-between gap-2 text-sm">
            {t("page.tools.liquid_emitter.look.tint")}
            <Input
              type="color"
              className="w-16 shrink-0"
              disabled={locked}
              value={tintToHex(look.tint)}
              onChange={(event) => update({ tint: hexToTint(event.currentTarget.value) })}
            />
          </label>
          <div className="space-y-1">
            <RangeField
              label={t("page.tools.liquid_emitter.look.absorption")}
              value={look.absorptionMm}
              defaultValue={DEFAULT_ABSORPTION_MM}
              min={2}
              max={60}
              step={1}
              display={`${look.absorptionMm} mm`}
              disabled={locked}
              onChange={(value) => update({ absorptionMm: value })}
            />
            <p className="text-xs text-muted-foreground">
              {t("page.tools.liquid_emitter.look.absorption_hint")}
            </p>
          </div>
        </>
      ) : null}
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          className="flex-1"
          disabled={locked || (offset === 0 && !look.customColor)}
          onClick={() => update({ strengthOffset: 0, customColor: false })}
        >
          {t("page.tools.liquid_emitter.look.reset")}
        </Button>
        <Button
          size="sm"
          className="flex-1"
          disabled={locked || !dirty}
          onClickPromise={async () => {
            if (await onSave(look)) setDraft(null);
          }}
        >
          {t("page.tools.liquid_emitter.look.save")}
        </Button>
      </div>
    </section>
  );
}

function anchorFromEmitter(emitter: Emitter): Anchor | null {
  const saved = emitter.anchor;
  const vec = (value: number[] | null | undefined) =>
    value?.length === 3 && value.every(Number.isFinite) ? (value as Vec3) : null;
  const corners = saved.trianglePositions;
  const vertices = vec(saved.faceVertices);
  const barycentric = vec(saved.barycentric);
  const point = vec(saved.point);
  const offset = vec(saved.offset);
  const direction = vec(saved.direction);
  if (
    !corners ||
    corners.length !== 9 ||
    !vertices ||
    !barycentric ||
    !point ||
    !offset ||
    !direction
  ) {
    return null;
  }
  return {
    meshId: saved.meshId,
    faceIndex: saved.faceIndex,
    vertices,
    triangle: [
      corners.slice(0, 3) as Vec3,
      corners.slice(3, 6) as Vec3,
      corners.slice(6, 9) as Vec3,
    ],
    barycentric,
    point,
    offset,
    direction,
  };
}

// Tint channels run from 0 to 1, as the runtime's own color setting stores them.
function tintToHex(tint: number[]) {
  const channels = [0, 1, 2].map((index) =>
    Math.round(Math.min(1, Math.max(0, tint[index] ?? 0)) * 255)
      .toString(16)
      .padStart(2, "0"),
  );
  return `#${channels.join("")}`;
}

function hexToTint(hex: string) {
  return [1, 3, 5].map((start) =>
    Number((parseInt(hex.slice(start, start + 2), 16) / 255).toFixed(3)),
  );
}

function limitOffset(offset: Vec3, limit: number): Vec3 {
  const length = Math.hypot(...offset);
  if (length <= limit) return offset;
  return offset.map((value) => (value * limit) / length) as Vec3;
}

const errorCodePattern = /LIQUID_([A-Z_]+)(?::([^\n]*))?/;

export function liquidErrorMessage(t: TFunction, error: unknown) {
  const message = toErrorMessage(error);
  const match = errorCodePattern.exec(message);
  if (!match) return message;
  const key = `page.tools.liquid_emitter.error.${match[1].toLowerCase()}`;
  const translated = t(key, { detail: match[2]?.trim() ?? "" });
  return translated === key ? message : translated;
}
