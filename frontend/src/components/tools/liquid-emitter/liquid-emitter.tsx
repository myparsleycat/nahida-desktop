import type { Emitter, Inspection } from "@bindings/tools/liquid_emitter";
import { InspectorSection } from "@renderer/components/tools/inspector-section";
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
import {
  anglesToDirection,
  type DirectionAngles,
  directionToAngles,
} from "@shared/liquid-emitter/direction-angles";
import type { Vec3 } from "@shared/liquid-emitter/flow-trajectory";
import type { ViewerForm, ViewerMeshTransport } from "@shared/mod-viewer/types";
import { toErrorMessage } from "@shared/utils";
import { isEqual } from "es-toolkit";
import type { TFunction } from "i18next";
import {
  CrosshairIcon,
  Loader2Icon,
  PlusIcon,
  Redo2Icon,
  Trash2Icon,
  TriangleAlertIcon,
  Undo2Icon,
} from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { followsRuntime, lookChanged, LookSection, useLookDraft } from "./liquid-emitter-look";
import { LiquidEmitterOverlay, type LiquidEmitterSurface } from "./liquid-emitter-overlay";
import {
  type ApplyMode,
  IN_PLACE,
  NEW_MOD,
  SLOT_COUNT_SETTING,
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
  /** The saved emitter the entries edit, or null while they place a new one. */
  pid: number | null;
  entries: Array<Anchor | null>;
  index: number;
  /** Consecutive edits with the same key, such as one slider drag, share one undo step. */
  lastKey?: string;
};

const HISTORY_LIMIT = 100;
// Within this tilt of straight down or up, the azimuth no longer turns the direction.
const POLE_DEGREES = 0.05;
// The runtime rejects a whole profile file when an offset axis exceeds 25 cm.
const MAX_OFFSET_METERS = 0.25;
const AXES = ["X", "Y", "Z"] as const;
const Z_DOWN: Vec3 = [0, 0, -1];
// Applying repairs an outdated or changed runtime, so only these leave editing open.
const EDITABLE_RUNTIME_STATES = new Set<string>(["current", "outdated", "modified"]);
// The backend's maxEmitters: each emitter takes a runtime slot while its character is drawn.
const MAX_EMITTERS = 4;
// How far the runtime's slot count goes up when a mod needs more slots than it has.
const SLOT_RAISE = 4;

export function LiquidEmitterTool({
  modPath,
  onModPathChange,
  onUnsavedChange,
}: {
  modPath: string;
  /** Called when the tool moves to another folder: a new mod it created, or the restored original. */
  onModPathChange?: (modPath: string) => void;
  /** Called with whether the anchor or look differs from what the mod has. */
  onUnsavedChange?: (unsaved: boolean) => void;
}) {
  const { t } = useTranslation();
  const session = useLiquidEmitterSession(modPath);
  const [mode, setMode] = useState<ApplyMode>(IN_PLACE);
  const [confirmRestore, setConfirmRestore] = useState(false);
  const [history, setHistory] = useState<History>({ pid: null, entries: [null], index: 0 });
  const [seeded, setSeeded] = useState(false);
  const [picking, setPicking] = useState(true);
  const [showTrajectory, setShowTrajectory] = useState(false);
  const [doubleSided, setDoubleSided] = useState(true);
  const [confirmRemoveAll, setConfirmRemoveAll] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [form, setForm] = useState(ALL_FORMS);
  const [visibleMeshes, setVisibleMeshes] = useState<ReadonlySet<string> | null>(null);
  const [slotPrompt, setSlotPrompt] = useState<{ current: number; next: number } | null>(null);
  // Straight down or up has no azimuth of its own, so the last one set is kept
  // for when the direction tilts away again.
  const [poleAzimuth, setPoleAzimuth] = useState(0);

  const inspection = session.inspection.data;
  const emitters = inspection?.emitters ?? [];
  // Anchors are edited in the mesh's own units, which are not meters in every game.
  const meshUnit = inspection?.meshUnit || 1;
  const down = inspection?.meshDown.length === 3 ? (inspection.meshDown as Vec3) : Z_DOWN;
  const meshes = session.source?.transport.meshes;
  const forms = session.source?.transport.forms ?? [];
  const formOfMesh = (meshId: string) => meshes?.find((mesh) => mesh.id === meshId)?.form;
  const editing = history.pid;
  const anchor = history.entries[history.index];
  const current =
    editing === null ? undefined : emitters.find((emitter) => emitter.pid === editing);
  const adding = current === undefined;
  const [draftLook, setDraftLook] = useLookDraft(`${inspection?.modPath}:${current?.pid}`);
  const savedLook = current?.look ?? inspection?.defaultLook;
  const lookDirty = lookChanged(draftLook, savedLook);
  const anchorDirty = !isEqual(anchor, current ? anchorFromEmitter(current) : null);
  const unsaved = anchorDirty || lookDirty;
  useEffect(() => {
    onUnsavedChange?.(unsaved);
  }, [onUnsavedChange, unsaved]);
  useEffect(() => () => onUnsavedChange?.(false), [onUnsavedChange]);
  const atLimit = emitters.length >= MAX_EMITTERS;
  const busy =
    session.apply.isPending ||
    session.remove.isPending ||
    session.install.isPending ||
    session.setLook.isPending ||
    session.raiseSlotCount.isPending ||
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
    const savedEmitter = emitters.find((emitter) => {
      const candidate = anchorFromEmitter(emitter);
      return candidate !== null && meshes.some((mesh) => mesh.id === candidate.meshId);
    });
    const saved = savedEmitter ? anchorFromEmitter(savedEmitter) : null;
    if (savedEmitter && saved) {
      setHistory({ pid: savedEmitter.pid, entries: [saved], index: 0 });
    }
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
      return { ...state, entries, index: entries.length - 1, lastKey: key };
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
  // Each emitter keeps its own undo history, so undoing never moves one emitter onto another's anchor.
  const select = (pid: number | null, next: Anchor | null) => {
    setHistory({ pid, entries: [next], index: 0 });
    setNotice(null);
  };
  // A request may finish after another emitter was selected, so it moves the
  // selection only while it still edits the emitter the request was for; a
  // null next drops the selection and its anchor together.
  const follow = (requested: number | null, next: number | null) =>
    setHistory((state) => {
      if (state.pid !== requested) return state;
      return next === null ? { pid: null, entries: [null], index: 0 } : { ...state, pid: next };
    });

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
      direction: anchor?.direction ?? down,
    });
  };

  const setOffsetAxis = (axis: number, meters: number, key: string) => {
    if (!anchor) return;
    const offset = [...anchor.offset] as Vec3;
    offset[axis] = meters / meshUnit;
    update(key, { offset: limitOffset(offset, MAX_OFFSET_METERS / meshUnit) });
  };

  const measured = anchor ? directionToAngles(anchor.direction, down) : undefined;
  const atPole =
    measured !== undefined && (measured.tilt < POLE_DEGREES || measured.tilt > 180 - POLE_DEGREES);
  const angles = measured && atPole ? { ...measured, azimuth: poleAzimuth } : measured;
  const setDirectionAngle = (key: keyof DirectionAngles, value: number) => {
    if (!angles) return;
    const next = { ...angles, [key]: value };
    setPoleAzimuth(next.azimuth);
    // Turning the azimuth of a vertical direction leaves the direction as it is.
    if (atPole && key === "azimuth") return;
    update(`direction-${key}`, { direction: anglesToDirection(next, down) });
  };

  const applyAnchor = () => {
    if (!anchor || !session.source) return;
    const requested = editing;
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
        pid: current?.pid ?? null,
        look: lookDirty ? draftLook : null,
      },
      {
        onSuccess: (result) => {
          if (applyMode !== NEW_MOD) {
            // A mod copied with its marker gets new slots on its first change,
            // so the edited emitter is followed by the slot the backend reports.
            follow(requested, result.applied);
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

  // Every emitter of a drawn character takes a runtime slot, so a new one past
  // the runtime's slot count would stay dry until the count goes up.
  const handleApply = () => {
    // A look alone needs no hooks, so it is saved even when the viewer no
    // longer draws the emitter's mesh; an emitter whose hook went missing
    // still goes through Apply, which restores it.
    if (current?.hooked && draftLook && lookDirty && !anchorDirty && applyMode === IN_PLACE) {
      const requested = current.pid;
      session.setLook.mutate(
        { pid: requested, look: draftLook },
        {
          onSuccess: (result) => {
            if (result.applied !== null) follow(requested, result.applied);
            toast.success(t("page.tools.liquid_emitter.applied"));
          },
          onError: (error) => toast.error(liquidErrorMessage(t, error)),
        },
      );
      return;
    }
    const setting = inspection?.runtime.settings?.find((entry) => entry.id === SLOT_COUNT_SETTING);
    const slots = Number(setting?.value);
    if (
      adding &&
      setting &&
      Number.isFinite(slots) &&
      emitters.length + 1 > slots &&
      slots < setting.max
    ) {
      setSlotPrompt({ current: slots, next: Math.min(slots + SLOT_RAISE, setting.max) });
      return;
    }
    applyAnchor();
  };

  const handleRaiseSlots = (next: number) => {
    if (!inspection) return;
    session.raiseSlotCount.mutate(
      { inspection, slotCount: next },
      {
        onSuccess: () => {
          toast.success(t("page.tools.liquid_emitter.slots.raised", { count: next }));
          applyAnchor();
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

  const handleRemove = (pids: number[]) => {
    const requested = editing;
    session.remove.mutate(pids, {
      onSuccess: (result) => {
        if (pids.length === 0 || (requested !== null && pids.includes(requested))) {
          follow(requested, null);
        } else if (requested !== null) {
          follow(
            requested,
            result.renumbered?.find((change) => change.from === requested)?.to ?? requested,
          );
        }
        toast.success(t("page.tools.liquid_emitter.removed"));
      },
      onError: (error) => toast.error(liquidErrorMessage(t, error)),
    });
  };

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

  const anchorMesh = anchor ? meshes?.find((mesh) => mesh.id === anchor.meshId) : undefined;
  const meshLabel = anchorMesh ? partLabel(anchorMesh, meshes) : (anchor?.meshId ?? "");
  const anchorForm = anchorMesh?.form;
  const anchorInForm = form === ALL_FORMS || !anchorForm || anchorForm === form;
  // A frame animation shows one mesh per frame, all of them the same draw for
  // the game, so a part counts as drawn while any mesh of its draw is visible.
  const visibleDraws = new Set(
    meshes?.flatMap((mesh) => (mesh.drawKey && visibleMeshes?.has(mesh.id) ? [mesh.drawKey] : [])),
  );
  // The anchor's own mesh comes first, so it keeps the marker while it is shown.
  const surfaceOf = (target: Anchor): LiquidEmitterSurface => {
    const drawKey = meshes?.find((mesh) => mesh.id === target.meshId)?.drawKey;
    const sameDraw = drawKey
      ? (meshes ?? []).flatMap((mesh) =>
          mesh.drawKey === drawKey && mesh.id !== target.meshId ? [mesh.id] : [],
        )
      : [];
    return {
      meshIds: [target.meshId, ...sameDraw],
      faceIndex: target.faceIndex,
      barycentric: target.barycentric,
    };
  };
  const drawn = (meshId: string) => {
    const drawKey = meshes?.find((mesh) => mesh.id === meshId)?.drawKey;
    return (
      visibleMeshes === null ||
      visibleMeshes.has(meshId) ||
      (drawKey !== undefined && visibleDraws.has(drawKey))
    );
  };
  // A toggle can swap the selected part for another drawn in the same place,
  // so the marker would otherwise point at a part the game is not drawing.
  // Another form tab hides the part on purpose and needs no warning.
  const anchorHidden = anchor !== null && anchorInForm && !drawn(anchor.meshId);
  const anchorShown = anchor !== null && anchorInForm && !anchorHidden;
  const position = anchor
    ? (anchor.point.map((value, axis) => value + anchor.offset[axis]) as Vec3)
    : undefined;
  const shown = (meshId: string) => {
    const meshForm = formOfMesh(meshId);
    return (form === ALL_FORMS || !meshForm || meshForm === form) && drawn(meshId);
  };
  const others = emitters.flatMap((emitter) => {
    const saved = emitter.pid === current?.pid ? null : anchorFromEmitter(emitter);
    return saved && shown(saved.meshId) ? [{ pid: emitter.pid, anchor: saved }] : [];
  });

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

      <div className="divide-y">
        <RuntimeSection
          inspection={inspection}
          error={session.inspection.error}
          busy={busy}
          installing={session.install.isPending}
          onInstall={handleInstall}
        />

        <EmitterList
          emitters={emitters}
          meshes={meshes}
          forms={forms}
          selectedPid={current?.pid}
          editing={
            anchor ? { label: meshLabel, form: anchorForm, faceIndex: anchor.faceIndex } : null
          }
          unsaved={unsaved}
          busy={busy}
          actions={
            <>
              <Button
                size="icon-xs"
                variant="ghost"
                disabled={!editable || busy || atLimit || (adding && !anchor)}
                aria-label={t("page.tools.liquid_emitter.selection.new")}
                title={t("page.tools.liquid_emitter.selection.new")}
                onClick={() => select(null, null)}
              >
                <PlusIcon />
              </Button>
              <Button
                size="icon-xs"
                variant="ghost"
                disabled={!editable || history.index === 0}
                aria-label={t("page.tools.liquid_emitter.history.undo")}
                title={t("page.tools.liquid_emitter.history.undo")}
                onClick={() => step(-1)}
              >
                <Undo2Icon />
              </Button>
              <Button
                size="icon-xs"
                variant="ghost"
                disabled={!editable || history.index >= history.entries.length - 1}
                aria-label={t("page.tools.liquid_emitter.history.redo")}
                title={t("page.tools.liquid_emitter.history.redo")}
                onClick={() => step(1)}
              >
                <Redo2Icon />
              </Button>
            </>
          }
          onSelect={(emitter) => {
            const saved = anchorFromEmitter(emitter);
            if (!saved) return;
            select(emitter.pid, saved);
            const savedForm = formOfMesh(saved.meshId);
            if (savedForm && form !== ALL_FORMS) setForm(savedForm);
          }}
          onRemove={(emitter) => handleRemove([emitter.pid])}
          onRemoveAll={() => setConfirmRemoveAll(true)}
        >
          {anchorHidden ? (
            <p className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-300">
              <TriangleAlertIcon className="mt-px size-3.5 shrink-0" aria-hidden="true" />
              {t("page.tools.liquid_emitter.selection.hidden")}
            </p>
          ) : null}
          {adding && atLimit ? (
            <p className="text-xs text-muted-foreground">
              {t("page.tools.liquid_emitter.selection.limit", { max: MAX_EMITTERS })}
            </p>
          ) : null}
          {notice ? <p className="text-xs text-destructive">{notice}</p> : null}
        </EmitterList>

        <InspectorSection
          id="liquid-emitter:offset"
          title={t("page.tools.liquid_emitter.offset.title")}
          summary={
            anchor
              ? anchor.offset.map((value) => (value * meshUnit * 1000).toFixed(1)).join(" · ")
              : undefined
          }
          className="px-4 py-3"
          panelClassName="space-y-3 pt-3"
        >
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
        </InspectorSection>

        <InspectorSection
          id="liquid-emitter:direction"
          title={t("page.tools.liquid_emitter.direction.title")}
          summary={
            angles
              ? angles.tilt < POLE_DEGREES
                ? t("page.tools.liquid_emitter.direction.summary_down")
                : `${angles.tilt.toFixed(0)}° · ${angles.azimuth.toFixed(0)}°`
              : undefined
          }
          className="px-4 py-3"
          panelClassName="space-y-3 pt-3"
        >
          <RangeField
            label={t("page.tools.liquid_emitter.direction.tilt")}
            value={angles?.tilt ?? 0}
            defaultValue={0}
            min={0}
            max={180}
            step={1}
            display={`${(angles?.tilt ?? 0).toFixed(0)}°`}
            disabled={locked}
            onChange={(value) => setDirectionAngle("tilt", value)}
          />
          <RangeField
            label={t("page.tools.liquid_emitter.direction.azimuth")}
            value={angles?.azimuth ?? 0}
            defaultValue={0}
            min={0}
            max={359}
            step={1}
            display={`${(angles?.azimuth ?? 0).toFixed(0)}°`}
            disabled={locked}
            onChange={(value) => setDirectionAngle("azimuth", value)}
          />
          <p className="text-xs text-muted-foreground">
            {t("page.tools.liquid_emitter.direction.hint")}
          </p>
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={locked}
            onClick={() => update("direction-down", { direction: down })}
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
        </InspectorSection>

        <InspectorSection
          id="liquid-emitter:position"
          title={t("page.tools.liquid_emitter.position.title")}
          className="px-4 py-3"
          panelClassName="space-y-2 pt-3"
        >
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
            {t("page.tools.liquid_emitter.position.hint", {
              axis: AXES[down.findIndex((value) => value !== 0)],
            })}
          </p>
        </InspectorSection>

        {savedLook ? (
          <LookSection
            saved={savedLook}
            draft={draftLook}
            onDraftChange={setDraftLook}
            locked={!editable || busy}
          />
        ) : null}

        <ApplyModeSection
          inspection={inspection}
          mode={mode}
          busy={busy}
          editable={editable}
          onModeChange={setMode}
          onRestore={() => setConfirmRestore(true)}
        />
      </div>
    </div>
  );

  // A new mod and an emitter whose hook went missing are worth applying unchanged.
  const applicable = unsaved || applyMode === NEW_MOD || current?.hooked === false;
  const inspectorFooter = (
    <div className="flex gap-2 rounded-md bg-muted/30 p-3">
      {current ? (
        <Button
          variant="outline"
          className="flex-1"
          disabled={busy}
          onClick={() => handleRemove([current.pid])}
        >
          {t("page.tools.liquid_emitter.remove")}
        </Button>
      ) : null}
      <Button
        className="flex-1"
        disabled={locked || busy || !applicable || (adding && atLimit)}
        isLoading={
          session.apply.isPending || session.raiseSlotCount.isPending || session.setLook.isPending
        }
        onClick={handleApply}
      >
        {t(
          applyMode === NEW_MOD
            ? "page.tools.liquid_emitter.mode.new_mod"
            : adding
              ? "page.tools.liquid_emitter.apply_new"
              : "page.tools.liquid_emitter.apply",
        )}
      </Button>
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
          <>
            {others.map((other) => (
              <LiquidEmitterOverlay
                key={other.pid}
                point={other.anchor.point}
                surface={surfaceOf(other.anchor)}
                offset={other.anchor.offset}
                direction={other.anchor.direction}
                down={down}
                meshUnit={meshUnit}
                showTrajectory={false}
                muted
              />
            ))}
            {anchor && anchorShown ? (
              <LiquidEmitterOverlay
                point={anchor.point}
                surface={surfaceOf(anchor)}
                offset={anchor.offset}
                direction={anchor.direction}
                down={down}
                meshUnit={meshUnit}
                force={(draftLook ?? savedLook)?.force}
                nozzle={(draftLook ?? savedLook)?.nozzle}
                showTrajectory={showTrajectory}
              />
            ) : null}
          </>
        }
        inspector={inspector}
        inspectorFooter={inspectorFooter}
        onVisibleMeshesChange={setVisibleMeshes}
      />

      <AlertDialog
        open={slotPrompt !== null}
        onOpenChange={(open) => {
          if (!open) setSlotPrompt(null);
        }}
      >
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.tools.liquid_emitter.slots.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.liquid_emitter.slots.description", {
                count: emitters.length + 1,
                current: slotPrompt?.current ?? 0,
                next: slotPrompt?.next ?? 0,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const next = slotPrompt?.next;
                setSlotPrompt(null);
                if (next !== undefined) handleRaiseSlots(next);
              }}
            >
              {t("page.tools.liquid_emitter.slots.confirm", { next: slotPrompt?.next ?? 0 })}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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

  // A generated mod has no mode to choose but must keep its way back visible.
  if (inspection.sourcePath) {
    return (
      <div className="p-4">
        <section className="space-y-2 rounded-md border bg-background/40 px-3 py-2">
          <h3 className="text-sm font-medium">{t("page.tools.liquid_emitter.generated.title")}</h3>
          <p className="text-xs break-all text-muted-foreground">
            {t("page.tools.liquid_emitter.generated.description", { path: inspection.sourcePath })}
          </p>
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={busy}
            onClick={onRestore}
          >
            {t("page.tools.liquid_emitter.generated.restore")}
          </Button>
        </section>
      </div>
    );
  }

  return (
    <InspectorSection
      id="liquid-emitter:mode"
      title={t("page.tools.liquid_emitter.mode.title")}
      summary={t(`page.tools.liquid_emitter.mode.${mode}`)}
      className="px-4 py-3"
      panelClassName="space-y-2 pt-3"
    >
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
    </InspectorSection>
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
  const problem = Boolean(error);
  const state = inspection?.runtime.state;
  // An installed runtime needs nothing from the user.
  if (!problem && (state === undefined || state === "current")) return null;

  const required = state === "missing" || state === "disabled";
  const actionable = !problem && (required || state === "outdated" || state === "modified");
  const description = problem
    ? liquidErrorMessage(t, error)
    : state === "foreign"
      ? t("page.tools.liquid_emitter.runtime.foreign_description", {
          path: inspection?.runtime.path,
        })
      : required
        ? t(`page.tools.liquid_emitter.runtime.required.${state}.description`)
        : null;

  return (
    <section className="space-y-2 px-4 py-3">
      <div className="flex items-center gap-2">
        <h3 className="min-w-0 flex-1 truncate text-sm font-medium">
          {t("page.tools.liquid_emitter.runtime.title")}
        </h3>
        {!problem && state ? (
          <Badge variant="destructive">
            {t(`page.tools.liquid_emitter.runtime.state.${state}`)}
          </Badge>
        ) : null}
        {actionable ? (
          <Button
            size="xs"
            variant={required ? "default" : "outline"}
            disabled={busy}
            isLoading={installing}
            onClick={onInstall}
          >
            {t(`page.tools.liquid_emitter.runtime.action.${state}`)}
          </Button>
        ) : null}
      </div>
      {description ? (
        <p
          className={cn(
            "text-xs break-all",
            problem || state === "foreign" ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {description}
        </p>
      ) : null}
    </section>
  );
}

function EmitterList({
  emitters,
  meshes,
  forms,
  selectedPid,
  editing,
  unsaved,
  busy,
  actions,
  onSelect,
  onRemove,
  onRemoveAll,
  children,
}: {
  emitters: Emitter[];
  meshes: ViewerMeshTransport[] | undefined;
  forms: ViewerForm[];
  /** The applied emitter being edited, or undefined while a new one is placed. */
  selectedPid: number | undefined;
  /** Where the anchor being edited sits now, which may differ from the applied one. */
  editing: { label: string; form: string | undefined; faceIndex: number } | null;
  unsaved: boolean;
  busy: boolean;
  actions: ReactNode;
  onSelect: (emitter: Emitter) => void;
  onRemove: (emitter: Emitter) => void;
  onRemoveAll: () => void;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  const adding = selectedPid === undefined;
  return (
    <InspectorSection
      id="liquid-emitter:emitters"
      title={t("page.tools.liquid_emitter.emitters.title", {
        count: emitters.length,
        max: MAX_EMITTERS,
      })}
      summary={adding ? t("page.tools.liquid_emitter.selection.adding") : `#${selectedPid}`}
      actions={
        <div className="flex items-center gap-0.5">
          {actions}
          {emitters.length > 0 ? (
            <Button size="xs" variant="ghost" disabled={busy} onClick={onRemoveAll}>
              {t("page.tools.liquid_emitter.remove_all.button")}
            </Button>
          ) : null}
        </div>
      }
      defaultOpen
      className="px-4 py-3"
      panelClassName="space-y-2 pt-2"
    >
      <ul className="space-y-1">
        {emitters.map((emitter) => {
          const mesh = meshes?.find((candidate) => candidate.id === emitter.anchor.meshId);
          const selected = emitter.pid === selectedPid;
          const shown = selected && editing;
          return (
            <li
              key={emitter.pid}
              className={cn(
                "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted/60",
                selected && "bg-muted",
              )}
            >
              <button
                type="button"
                className="min-w-0 flex-1 text-left disabled:cursor-not-allowed"
                disabled={!mesh || busy}
                title={mesh ? undefined : t("page.tools.liquid_emitter.emitters.missing_mesh")}
                onClick={() => onSelect(emitter)}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate">
                    {shown ? editing.label : mesh ? partLabel(mesh, meshes) : emitter.section}
                  </span>
                  <FormBadge
                    forms={forms}
                    form={shown ? editing.form : (mesh?.form ?? iniFolder(emitter.iniPath))}
                  />
                </span>
                <span className="block truncate text-xs text-muted-foreground">
                  {emitter.iniPath} · #{emitter.pid} ·{" "}
                  {t(
                    followsRuntime(emitter.look)
                      ? "page.tools.liquid_emitter.emitters.look_runtime"
                      : "page.tools.liquid_emitter.emitters.look_custom",
                  )}
                </span>
              </button>
              {selected && unsaved ? (
                <Badge variant="outline">{t("page.tools.liquid_emitter.emitters.unsaved")}</Badge>
              ) : null}
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
        {adding ? (
          <li className="rounded-md bg-muted px-2 py-1.5 text-sm">
            <span className="flex min-w-0 items-center gap-2">
              <span className="truncate">{t("page.tools.liquid_emitter.selection.adding")}</span>
              <FormBadge forms={forms} form={editing?.form} />
            </span>
            <span className="block truncate text-xs text-muted-foreground">
              {editing
                ? `${editing.label} · ${t("page.tools.liquid_emitter.selection.triangle", { index: editing.faceIndex })}`
                : t("page.tools.liquid_emitter.selection.empty")}
            </span>
          </li>
        ) : null}
      </ul>
      {children}
    </InspectorSection>
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

// One override section can draw several parts, which then share its name;
// the draw number that ends the mesh ID tells them apart.
function partLabel(mesh: ViewerMeshTransport, meshes: ViewerMeshTransport[] | undefined) {
  const shared = meshes?.some(
    (other) =>
      other.id !== mesh.id && other.component === mesh.component && other.form === mesh.form,
  );
  return shared
    ? `${mesh.component} #${mesh.id.slice(mesh.id.lastIndexOf(":") + 1)}`
    : mesh.component;
}

// The folder of a mod-relative INI path, as the viewer names forms.
function iniFolder(iniPath: string) {
  const slash = iniPath.lastIndexOf("/");
  return slash < 0 ? "." : iniPath.slice(0, slash);
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
