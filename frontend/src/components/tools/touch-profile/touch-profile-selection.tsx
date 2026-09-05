import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
} from "@renderer/components/ui/combobox";
import { FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Slider } from "@renderer/components/ui/slider";
import { cn } from "@renderer/lib/utils";
import { Loader2Icon } from "lucide-react";

import type { TouchProfileControls } from "./use-touch-profile-controls";
import type { TouchProfileSession } from "./use-touch-profile-session";
const BONE_WEIGHT_THRESHOLD_RANGE = { min: 0, max: 1, step: 0.005 } as const;
const TOUCH_ZONE_CHANNEL_COUNT = 12;
const CHANNEL_LABELS = [
  "L Breast",
  "R Breast",
  "L Butt/Thigh",
  "R Butt/Thigh",
  ...Array.from({ length: TOUCH_ZONE_CHANNEL_COUNT - 4 }, (_, i) => `Ch ${i + 4}`),
] as const;
export function TouchProfileSelection({
  session,
  controls,
}: {
  session: TouchProfileSession;
  controls: TouchProfileControls;
}) {
  const {
    t,
    loading,
    inspection,
    selectedMeshIds,
    setSelectedMeshIds,
    selectedMeshId,
    setSelectedMeshId,
    analysisMode,
    weightThreshold,
    setWeightThreshold,
    boneZoneAssignments,
    setBoneZoneAssignments,
    analyzeSelected,
  } = session;
  const { boneHoverRef, channelSelectBoneRef, handleBoneHighlight, syncAssignmentBonePreview } =
    controls;
  if (session.phase !== "select" || !inspection) return null;
  return (
    <>
      <div>
        <div className="text-sm font-medium">{t("page.tools.touch_profile.mesh_select_title")}</div>
        <div className="mt-2 flex gap-3 text-xs text-muted-foreground">
          <span>
            {t("page.tools.touch_profile.support_grade")}: {inspection.supportGrade}
          </span>
          <span>
            {t("page.tools.touch_profile.mesh_select_count", {
              selected: selectedMeshIds.size,
              total: inspection.components.length,
            })}
          </span>
        </div>
      </div>

      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={() => {
            setSelectedMeshIds(new Set(inspection.components.map((component) => component.id)));
          }}
        >
          {t("page.tools.touch_profile.mesh_select_all")}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={() => {
            setSelectedMeshIds(
              new Set(
                inspection.components
                  .filter((component) => component.interactiveCandidate)
                  .map((component) => component.id),
              ),
            );
          }}
        >
          {t("page.tools.touch_profile.mesh_select_candidates")}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 text-xs"
          onClick={() => setSelectedMeshIds(new Set())}
        >
          {t("page.tools.touch_profile.mesh_select_none")}
        </Button>
      </div>

      <div className="space-y-2">
        {inspection.components.map((component) => (
          <div
            key={component.id}
            className={cn(
              "w-full rounded-md border px-3 py-2 transition-colors",
              selectedMeshId === component.id
                ? "border-primary bg-primary/10"
                : "border-border/60 hover:bg-muted/60",
            )}
          >
            <div className="flex items-center gap-2">
              <Checkbox
                checked={selectedMeshIds.has(component.id)}
                onCheckedChange={(checked) => {
                  setSelectedMeshIds((current) => {
                    const next = new Set(current);
                    if (checked) next.add(component.id);
                    else next.delete(component.id);
                    return next;
                  });
                }}
                aria-label={component.name}
              />
              <button
                type="button"
                className="min-w-0 flex-1 text-left"
                onClick={() => {
                  setSelectedMeshId((current) => (current === component.id ? "" : component.id));
                }}
              >
                <div className="truncate text-sm font-medium">
                  {component.name}
                  {component.variantKey ? (
                    <span className="text-xs text-muted-foreground">
                      {" "}
                      (v{component.variantKey})
                    </span>
                  ) : null}
                </div>
                <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <Badge variant="outline">{component.kind}</Badge>
                  <Badge variant="outline">{component.supportGrade}</Badge>
                  <span>
                    {component.vertexCount.toLocaleString()} v ·{" "}
                    {component.indexCount.toLocaleString()} i
                  </span>
                  {!component.interactiveCandidate ? (
                    <Badge variant="secondary">
                      {t("page.tools.touch_profile.mesh_non_candidate")}
                    </Badge>
                  ) : null}
                </div>
              </button>
            </div>
          </div>
        ))}
      </div>

      {analysisMode === "bone" ? (
        <div className="space-y-3 rounded-md border border-border/60 p-3">
          <div>
            <div className="text-sm font-medium">
              {t("page.tools.touch_profile.bone_select_title")}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">
              {t("page.tools.touch_profile.bone_select_hint")}
            </div>
          </div>
          <div>
            <FieldLabel>{t("page.tools.touch_profile.weight_threshold")}</FieldLabel>
            <div className="mt-1 flex items-center gap-3">
              <Slider
                value={weightThreshold}
                min={BONE_WEIGHT_THRESHOLD_RANGE.min}
                max={BONE_WEIGHT_THRESHOLD_RANGE.max}
                step={BONE_WEIGHT_THRESHOLD_RANGE.step}
                onValueChange={(values) => setWeightThreshold([values[0], values[1]])}
                className="flex-1"
              />
              <span className="w-20 text-right text-xs tabular-nums">
                {weightThreshold[0].toFixed(3)} ~ {weightThreshold[1].toFixed(3)}
              </span>
            </div>
            <div className="mt-1 text-xs text-muted-foreground">
              {t("page.tools.touch_profile.weight_threshold_hint")}
            </div>
          </div>
          {inspection.components
            .filter((c) => selectedMeshIds.has(c.id))
            .map((component) => {
              const assignments = boneZoneAssignments[component.id] ?? [];
              const hasBlend = component.hasBlend;
              const boneItems = component.bones.map((bone) => ({
                key: `bone:${bone.id}`,
                label: t("page.tools.touch_profile.bone_label", {
                  id: bone.id,
                  count: bone.vertexCount,
                }),
              }));
              const selectedBoneItems = boneItems.filter((item) =>
                assignments.some((a) => `bone:${a.boneId}` === item.key),
              );
              return (
                <div key={component.id} className="space-y-2">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <span>{component.name}</span>
                    {!hasBlend ? (
                      <Badge variant="destructive">{t("page.tools.touch_profile.no_blend")}</Badge>
                    ) : (
                      <Badge variant="outline">
                        {component.bones.length} {t("page.tools.touch_profile.bones_count")}
                      </Badge>
                    )}
                  </div>
                  {hasBlend ? (
                    <>
                      <Combobox
                        multiple
                        autoHighlight
                        items={boneItems}
                        value={selectedBoneItems}
                        onValueChange={(next) => {
                          const nextKeys = (next ?? []).map((item) => item.key);
                          const nextAssignments = nextKeys.map((key) => {
                            const boneId = Number(key.slice(5));
                            const existing = assignments.find((a) => a.boneId === boneId);
                            if (existing) return existing;
                            return { boneId, channel: null };
                          });
                          setBoneZoneAssignments((current) => ({
                            ...current,
                            [component.id]: nextAssignments,
                          }));
                        }}
                        onItemHighlighted={(item) => {
                          const boneId = item ? Number(item.key.slice(5)) : null;
                          handleBoneHighlight(boneId);
                        }}
                        onOpenChange={(open) => {
                          if (!open) handleBoneHighlight(null);
                        }}
                        itemToStringLabel={(item) => item.label}
                        isItemEqualToValue={(a, b) => a.key === b.key}
                      >
                        <ComboboxChips>
                          <ComboboxValue>
                            {(value: typeof boneItems) => (
                              <>
                                {value.map((item) => {
                                  const boneId = Number(item.key.slice(5));
                                  return (
                                    <ComboboxChip
                                      key={item.key}
                                      onMouseEnter={() => handleBoneHighlight(boneId)}
                                      onMouseLeave={() => handleBoneHighlight(null)}
                                    >
                                      {item.label}
                                    </ComboboxChip>
                                  );
                                })}
                                <ComboboxInput
                                  placeholder={
                                    value.length > 0
                                      ? ""
                                      : t("page.tools.touch_profile.bone_select_placeholder")
                                  }
                                />
                              </>
                            )}
                          </ComboboxValue>
                        </ComboboxChips>
                        <ComboboxContent>
                          <ComboboxEmpty>{t("page.tools.touch_profile.no_bones")}</ComboboxEmpty>
                          <ComboboxList>
                            {(item: (typeof boneItems)[number]) => (
                              <ComboboxItem key={item.key} value={item}>
                                {item.label}
                              </ComboboxItem>
                            )}
                          </ComboboxList>
                        </ComboboxContent>
                      </Combobox>

                      {assignments.length > 0 ? (
                        <div className="space-y-1.5">
                          {assignments.map((assignment, ai) => {
                            return (
                              <div
                                key={`${assignment.boneId}-${ai}`}
                                className="flex items-center gap-2 rounded-md border border-border/40 px-2 py-1.5"
                                onMouseEnter={() => {
                                  boneHoverRef.current = assignment.boneId;
                                  syncAssignmentBonePreview();
                                }}
                                onMouseLeave={() => {
                                  if (boneHoverRef.current === assignment.boneId) {
                                    boneHoverRef.current = null;
                                  }
                                  syncAssignmentBonePreview();
                                }}
                              >
                                <span className="min-w-0 flex-1 truncate text-xs font-medium">
                                  {t("page.tools.touch_profile.bone_label", {
                                    id: assignment.boneId,
                                    count:
                                      component.bones.find((b) => b.id === assignment.boneId)
                                        ?.vertexCount ?? 0,
                                  })}
                                </span>
                                <Select
                                  value={
                                    assignment.channel === null ? null : String(assignment.channel)
                                  }
                                  items={Array.from(
                                    { length: TOUCH_ZONE_CHANNEL_COUNT },
                                    (_, ch) => ({
                                      value: String(ch),
                                      label: CHANNEL_LABELS[ch],
                                    }),
                                  )}
                                  onValueChange={(value) => {
                                    const channel = value === null ? null : Number(value);
                                    setBoneZoneAssignments((current) => ({
                                      ...current,
                                      [component.id]: (current[component.id] ?? []).map((a, idx) =>
                                        idx === ai ? { ...a, channel } : a,
                                      ),
                                    }));
                                  }}
                                  onOpenChange={(open) => {
                                    channelSelectBoneRef.current = open ? assignment.boneId : null;
                                    syncAssignmentBonePreview();
                                  }}
                                >
                                  <SelectTrigger className="h-7 w-28 text-xs">
                                    <SelectValue
                                      placeholder={t(
                                        "page.tools.touch_profile.channel_select_placeholder",
                                      )}
                                    />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectGroup>
                                      {Array.from({ length: TOUCH_ZONE_CHANNEL_COUNT }, (_, ch) => {
                                        const otherBone = assignments.find(
                                          (a, idx) => idx !== ai && a.channel === ch,
                                        );
                                        return (
                                          <SelectItem
                                            key={ch}
                                            value={String(ch)}
                                            className="text-xs"
                                          >
                                            {CHANNEL_LABELS[ch]}
                                            {otherBone
                                              ? ` (${t(
                                                  "page.tools.touch_profile.bone_label_short",
                                                  { id: otherBone.boneId },
                                                )})`
                                              : ""}
                                          </SelectItem>
                                        );
                                      })}
                                    </SelectGroup>
                                  </SelectContent>
                                </Select>
                                <Input
                                  value={assignment.label ?? ""}
                                  placeholder={t("page.tools.touch_profile.zone_label_placeholder")}
                                  onChange={(e) => {
                                    const label = e.target.value;
                                    setBoneZoneAssignments((current) => ({
                                      ...current,
                                      [component.id]: (current[component.id] ?? []).map((a, idx) =>
                                        idx === ai ? { ...a, label: label || undefined } : a,
                                      ),
                                    }));
                                  }}
                                  className="h-7 w-28 text-xs"
                                />
                              </div>
                            );
                          })}
                        </div>
                      ) : null}
                    </>
                  ) : (
                    <div className="text-xs text-muted-foreground">
                      {t("page.tools.touch_profile.no_blend_hint")}
                    </div>
                  )}
                </div>
              );
            })}
        </div>
      ) : null}

      <Button
        type="button"
        onClick={() => void analyzeSelected()}
        disabled={
          loading ||
          selectedMeshIds.size === 0 ||
          (analysisMode === "bone" &&
            (![...selectedMeshIds].some((id) => (boneZoneAssignments[id] ?? []).length > 0) ||
              [...selectedMeshIds].some((id) =>
                (boneZoneAssignments[id] ?? []).some((a) => a.channel === null),
              )))
        }
      >
        {loading ? <Loader2Icon className="mr-2 size-4 animate-spin" /> : null}
        {loading
          ? t("page.tools.touch_profile.analyzing")
          : t("page.tools.touch_profile.mesh_next")}
      </Button>
    </>
  );
}
