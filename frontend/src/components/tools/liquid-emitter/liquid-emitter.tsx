import type { Emitter, Inspection, Look } from "@bindings/tools/liquid_emitter";
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
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { followsRuntime, LookSection, useLookDraft } from "./liquid-emitter-look";
import { LiquidEmitterOverlay } from "./liquid-emitter-overlay";
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
// The runtime rejects a whole profile file when an offset axis exceeds 25 cm.
const MAX_OFFSET_METERS = 0.25;
const AXES = ["X", "Y", "Z"] as const;
const DOWN: Vec3 = [0, 0, -1];
// Applying repairs an outdated or changed runtime, so only these leave editing open.
const EDITABLE_RUNTIME_STATES = new Set<string>(["current", "outdated", "modified"]);
// The backend's maxEmitters: each emitter takes a runtime slot while its character is drawn.
const MAX_EMITTERS = 4;
// How far the runtime's slot count goes up when a mod needs more slots than it has.
const SLOT_RAISE = 4;

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

  const inspection = session.inspection.data;
  const emitters = inspection?.emitters ?? [];
  // Anchors are edited in the mesh's own units, which are not meters in every game.
  const meshUnit = inspection?.meshUnit || 1;
  const meshes = session.source?.transport.meshes;
  const forms = session.source?.transport.forms ?? [];
  const formOfMesh = (meshId: string) => meshes?.find((mesh) => mesh.id === meshId)?.form;
  const editing = history.pid;
  const anchor = history.entries[history.index];
  const current =
    editing === null ? undefined : emitters.find((emitter) => emitter.pid === editing);
  const adding = current === undefined;
  const [draftLook, setDraftLook] = useLookDraft(`${inspection?.modPath}:${current?.pid}`);
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

  const handleSaveLook = (pid: number, look: Look) =>
    session.setLook.mutateAsync({ pid, look }).then(
      (result) => {
        if (result.applied !== null) follow(pid, result.applied);
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

  const anchorMesh = anchor ? meshes?.find((mesh) => mesh.id === anchor.meshId) : undefined;
  const meshLabel = anchorMesh ? partLabel(anchorMesh, meshes) : (anchor?.meshId ?? "");
  const anchorForm = anchorMesh?.form;
  const anchorInForm = form === ALL_FORMS || !anchorForm || anchorForm === form;
  // A toggle can swap the selected part for another drawn in the same place,
  // so the marker would otherwise point at a part the game is not drawing.
  // Another form tab hides the part on purpose and needs no warning.
  const anchorHidden =
    anchor !== null && anchorInForm && visibleMeshes !== null && !visibleMeshes.has(anchor.meshId);
  const anchorShown = anchor !== null && anchorInForm && !anchorHidden;
  const position = anchor
    ? (anchor.point.map((value, axis) => value + anchor.offset[axis]) as Vec3)
    : undefined;
  const shown = (meshId: string) => {
    const meshForm = formOfMesh(meshId);
    return (
      (form === ALL_FORMS || !meshForm || meshForm === form) &&
      (visibleMeshes === null || visibleMeshes.has(meshId))
    );
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
        <div className="space-y-4 p-4">
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
                  disabled={!editable || busy || atLimit || (adding && !anchor)}
                  aria-label={t("page.tools.liquid_emitter.selection.new")}
                  title={t("page.tools.liquid_emitter.selection.new")}
                  onClick={() => select(null, null)}
                >
                  <PlusIcon />
                </Button>
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
              <div className="mb-1 text-xs text-muted-foreground">
                {current
                  ? t("page.tools.liquid_emitter.selection.editing", { pid: current.pid })
                  : t("page.tools.liquid_emitter.selection.adding")}
              </div>
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
                  {anchorHidden ? (
                    <p className="mt-1.5 flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-300">
                      <TriangleAlertIcon className="mt-px size-3.5 shrink-0" aria-hidden="true" />
                      {t("page.tools.liquid_emitter.selection.hidden")}
                    </p>
                  ) : null}
                </>
              ) : (
                <span className="text-muted-foreground">
                  {t("page.tools.liquid_emitter.selection.empty")}
                </span>
              )}
            </div>
            {adding && atLimit ? (
              <p className="text-xs text-muted-foreground">
                {t("page.tools.liquid_emitter.selection.limit", { max: MAX_EMITTERS })}
              </p>
            ) : null}
            {notice ? <p className="text-xs text-destructive">{notice}</p> : null}
          </section>
        </div>

        <InspectorSection
          id="liquid-emitter:offset"
          title={t("page.tools.liquid_emitter.offset.title")}
          summary={
            anchor
              ? anchor.offset.map((value) => (value * meshUnit * 1000).toFixed(1)).join(" · ")
              : undefined
          }
          defaultOpen
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
            anchor
              ? t(
                  isEqual(anchor.direction, DOWN)
                    ? "page.tools.liquid_emitter.direction.summary_down"
                    : "page.tools.liquid_emitter.direction.summary_custom",
                )
              : undefined
          }
          className="px-4 py-3"
          panelClassName="space-y-2 pt-3"
        >
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
            {t("page.tools.liquid_emitter.position.hint")}
          </p>
        </InspectorSection>

        <ApplyModeSection
          inspection={inspection}
          mode={mode}
          busy={busy}
          editable={editable}
          onModeChange={setMode}
          onRestore={() => setConfirmRestore(true)}
        />

        <EmitterList
          emitters={emitters}
          meshes={meshes}
          forms={forms}
          selectedPid={current?.pid}
          busy={busy}
          onSelect={(emitter) => {
            const saved = anchorFromEmitter(emitter);
            if (!saved) return;
            select(emitter.pid, saved);
            const savedForm = formOfMesh(saved.meshId);
            if (savedForm && form !== ALL_FORMS) setForm(savedForm);
          }}
          onRemove={(emitter) => handleRemove([emitter.pid])}
          onRemoveAll={() => setConfirmRemoveAll(true)}
        />

        <LookSection
          emitter={current}
          draft={draftLook}
          onDraftChange={setDraftLook}
          busy={busy}
          editable={editable}
          onSave={(look) => (current ? handleSaveLook(current.pid, look) : Promise.resolve(false))}
        />
      </div>
    </div>
  );

  const inspectorFooter = (
    <div className="space-y-2 rounded-md bg-muted/30 p-3">
      <Button
        className="w-full"
        disabled={locked || busy || (adding && atLimit)}
        isLoading={session.apply.isPending || session.raiseSlotCount.isPending}
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
                offset={other.anchor.offset}
                direction={other.anchor.direction}
                meshUnit={meshUnit}
                showTrajectory={false}
                muted
              />
            ))}
            {anchor && anchorShown ? (
              <LiquidEmitterOverlay
                point={anchor.point}
                offset={anchor.offset}
                direction={anchor.direction}
                meshUnit={meshUnit}
                force={(draftLook ?? current?.look)?.force}
                nozzle={(draftLook ?? current?.look)?.nozzle}
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
  selectedPid,
  busy,
  onSelect,
  onRemove,
  onRemoveAll,
}: {
  emitters: Emitter[];
  meshes: ViewerMeshTransport[] | undefined;
  forms: ViewerForm[];
  selectedPid: number | undefined;
  busy: boolean;
  onSelect: (emitter: Emitter) => void;
  onRemove: (emitter: Emitter) => void;
  onRemoveAll: () => void;
}) {
  const { t } = useTranslation();
  return (
    <InspectorSection
      id="liquid-emitter:emitters"
      title={t("page.tools.liquid_emitter.emitters.title", {
        count: emitters.length,
        max: MAX_EMITTERS,
      })}
      actions={
        emitters.length > 0 ? (
          <Button size="xs" variant="ghost" disabled={busy} onClick={onRemoveAll}>
            {t("page.tools.liquid_emitter.remove_all.button")}
          </Button>
        ) : null
      }
      defaultOpen
      className="px-4 py-3"
      panelClassName="pt-2"
    >
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
                  emitter.pid === selectedPid && "bg-muted",
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
                      {mesh ? partLabel(mesh, meshes) : emitter.section}
                    </span>
                    <FormBadge forms={forms} form={mesh?.form ?? iniFolder(emitter.iniPath)} />
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
