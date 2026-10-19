import type { Emitter, Inspection } from "@bindings/tools/liquid_emitter";
import type { ModelViewerSurfacePick } from "@renderer/components/tools/model-viewer/model-viewer-contract";
import { ModelViewerWorkspace } from "@renderer/components/tools/model-viewer/model-viewer-workspace";
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
import { toErrorMessage } from "@shared/utils";
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

  const inspection = session.inspection.data;
  const emitters = inspection?.emitters ?? [];
  const meshes = session.source?.transport.meshes;
  const anchor = history.entries[history.index];
  const current = anchor
    ? emitters.find((emitter) => emitter.anchor.meshId === anchor.meshId)
    : undefined;
  const busy =
    session.apply.isPending ||
    session.remove.isPending ||
    session.install.isPending ||
    session.restore.isPending;
  // A mod this tool generated is always edited in place.
  const applyMode = inspection?.sourcePath ? IN_PLACE : mode;

  // Start from the first saved emitter whose mesh the viewer still has.
  if (!seeded && inspection && meshes) {
    setSeeded(true);
    const saved = emitters
      .map(anchorFromEmitter)
      .find((candidate) => candidate && meshes.some((mesh) => mesh.id === candidate.meshId));
    if (saved) setHistory({ entries: [saved], index: 0 });
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
    offset[axis] = meters;
    update(key, { offset: limitOffset(offset) });
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
  const position = anchor
    ? (anchor.point.map((value, axis) => value + anchor.offset[axis]) as Vec3)
    : undefined;

  const inspector = (
    <div className="rounded-md bg-muted/30">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-3">
        <div className="text-sm font-medium">{t("page.tools.liquid_emitter.title")}</div>
        <Button
          size="sm"
          variant={picking ? "default" : "outline"}
          aria-pressed={picking}
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
                disabled={history.index === 0}
                aria-label={t("page.tools.liquid_emitter.history.undo")}
                title={t("page.tools.liquid_emitter.history.undo")}
                onClick={() => step(-1)}
              >
                <Undo2Icon />
              </Button>
              <Button
                size="icon-sm"
                variant="ghost"
                disabled={history.index >= history.entries.length - 1}
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
                <div className="truncate font-medium" title={meshLabel}>
                  {meshLabel}
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
                  disabled={!anchor}
                  value={position ? Number(position[axis].toFixed(4)) : ""}
                  onChange={(event) => {
                    const value = Number(event.currentTarget.value);
                    if (anchor && Number.isFinite(value)) {
                      setOffsetAxis(axis, value - anchor.point[axis], `position-${axis}`);
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
            const millimeters = (anchor?.offset[axis] ?? 0) * 1000;
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
                onChange={(value) => setOffsetAxis(axis, value / 1000, `offset-${axis}`)}
              />
            );
          })}
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={!anchor}
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
                  disabled={!anchor}
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
            disabled={!anchor}
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
          onModeChange={setMode}
          onRestore={() => setConfirmRestore(true)}
        />

        <section className="space-y-2">
          <Button
            className="w-full"
            disabled={!anchor || busy || inspection?.runtime.state === "foreign"}
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
          selectedMeshId={anchor?.meshId}
          busy={busy}
          onSelect={(emitter) => {
            const saved = anchorFromEmitter(emitter);
            if (saved) commit(saved);
          }}
          onRemove={(emitter) => handleRemove([emitter.pid])}
          onRemoveAll={() => setConfirmRemoveAll(true)}
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
        onSurfacePick={picking ? handlePick : undefined}
        overlay={
          anchor ? (
            <LiquidEmitterOverlay
              point={anchor.point}
              offset={anchor.offset}
              direction={anchor.direction}
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
  onModeChange,
  onRestore,
}: {
  inspection: Inspection | undefined;
  mode: ApplyMode;
  busy: boolean;
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
            disabled={busy}
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
  onInstall,
}: {
  inspection: Inspection | undefined;
  error: unknown;
  busy: boolean;
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
  selectedMeshId,
  busy,
  onSelect,
  onRemove,
  onRemoveAll,
}: {
  emitters: Emitter[];
  meshes: Array<{ id: string; component: string }> | undefined;
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
                  <span className="block truncate">{mesh?.component ?? emitter.section}</span>
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

function limitOffset(offset: Vec3): Vec3 {
  const length = Math.hypot(...offset);
  if (length <= MAX_OFFSET_METERS) return offset;
  return offset.map((value) => (value * MAX_OFFSET_METERS) / length) as Vec3;
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
