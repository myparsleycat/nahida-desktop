import { Mod } from "@bindings/mod";
import { KeyRecorder } from "@renderer/components/mod/key-recorder";
import { Alert, AlertDescription } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@renderer/components/ui/dialog";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { Kbd } from "@renderer/components/ui/kbd";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Switch } from "@renderer/components/ui/switch";
import {
  buildDefaultPlan,
  canUseClassic,
  planHasClassicViolation,
  planIsValid,
} from "@renderer/lib/mod-merge";
import { useModStore } from "@renderer/store/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { stripDisabledPrefix } from "@shared/mod";
import type {
  ClassifyMergePacksResult,
  MergeEngine,
  MergePackClassification,
  MergePlacement,
  MergePlanGroup,
  MergePlanNode,
} from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownIcon, ArrowUpIcon, GroupIcon, Loader2Icon, UngroupIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function MergeModsDialog() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const open = useModStore((s) => s.isMergeDialogOpen);
  const setOpen = useModStore((s) => s.setMergeDialogOpen);
  const selectedModPaths = useModStore((s) => s.selectedModPaths);
  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const exitMergeMode = useModStore((s) => s.exitMergeMode);

  const selectedPathsArray = useMemo(() => Array.from(selectedModPaths), [selectedModPaths]);

  const {
    data: classification,
    isLoading,
    error: classificationError,
  } = useQuery({
    queryKey: ["mod:classifyMergePacks", selectedPathsArray],
    queryFn: () => Mod.ClassifyMergePacks(selectedPathsArray),
    enabled: open && selectedPathsArray.length > 0,
    retry: false,
  });

  useEffect(() => {
    if (!open || !classificationError) return;
    toast.error(toErrorMessage(classificationError) || t("page.mod.merge.classify_failed"));
    setOpen(false);
  }, [classificationError, open, setOpen, t]);

  const [userPlan, setUserPlan] = useState<MergePlanGroup | null>(null);
  const [userPackName, setUserPackName] = useState<string | null>(null);
  const [placement, setPlacement] = useState<MergePlacement>("new_folder");
  const [isMerging, setIsMerging] = useState(false);
  const [editingKey, setEditingKey] = useState<"forward" | "back" | null>(null);
  const [prevClassification, setPrevClassification] = useState(classification);

  if (classification !== prevClassification) {
    setPrevClassification(classification);
    setUserPlan(null);
    setUserPackName(null);
  }

  const defaultPackName = useMemo(() => {
    if (!classification) return "Merged";
    return (
      stripDisabledPrefix(classification.packs?.[0]?.name ?? "Merged").replace(/\s+/g, "") ||
      "Merged"
    );
  }, [classification]);

  const defaultPlan = useMemo(() => {
    if (!classification) return null;
    return buildDefaultPlan(classification as ClassifyMergePacksResult, defaultPackName);
  }, [classification, defaultPackName]);

  const packName = userPackName ?? defaultPackName;
  const plan = userPlan ?? defaultPlan;

  const setPackName = (name: string) => setUserPackName(name);
  const setPlan = (
    nextPlan: MergePlanGroup | ((prev: MergePlanGroup | null) => MergePlanGroup | null),
  ) => {
    setUserPlan((prev) =>
      typeof nextPlan === "function" ? nextPlan(prev ?? defaultPlan) : nextPlan,
    );
  };

  const packsByPath = useMemo(
    () =>
      new Map(
        (classification?.packs ?? []).map((pack) => [pack.path, pack as MergePackClassification]),
      ),
    [classification],
  );

  const classicLocked = plan ? planHasClassicViolation(plan, packsByPath) : false;
  const canSubmit = Boolean(plan && selectedGroupPath && planIsValid(plan) && !classicLocked);

  const handleMerge = async () => {
    if (!plan || !selectedGroupPath) return;
    setIsMerging(true);
    try {
      await Mod.MergeMods({
        groupPath: selectedGroupPath,
        placement,
        packName,
        root: { ...plan, name: packName.replace(/\s+/g, "") || plan.name },
      });
      await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
      toast.success(t("page.mod.merge.success"));
      exitMergeMode();
    } catch (error) {
      const errorMessage = toErrorMessage(error);
      if (errorMessage.includes("MOD_DOWNLOAD_IN_PROGRESS")) {
        toast.warning(t("page.mod.download_action_blocked"));
      } else {
        toast.error(errorMessage || t("page.mod.merge.failed"));
      }
    } finally {
      setIsMerging(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex h-[min(90vh,800px)] w-[min(48rem,calc(100%-2rem))] max-w-none grid-rows-[auto_minmax(0,1fr)_auto] flex-col overflow-hidden sm:max-w-none">
          <DialogHeader>
            <DialogTitle>{t("page.mod.merge.dialog_title")} (Beta)</DialogTitle>
          </DialogHeader>

          {isLoading || !classification ? (
            <div className="flex items-center justify-center py-10 text-muted-foreground">
              <Loader2Icon className="mr-2 size-4 animate-spin" />
              {t("page.mod.merge.classifying")}
            </div>
          ) : !plan ? (
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 py-10">
              <Alert variant="destructive">
                <AlertDescription>{t("page.mod.merge.no_usable_mods")}</AlertDescription>
              </Alert>
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden">
              {!classification.hashOverlap && (
                <Alert>
                  <AlertDescription>{t("page.mod.merge.hash_mismatch")}</AlertDescription>
                </Alert>
              )}
              {classicLocked && (
                <Alert variant="destructive">
                  <AlertDescription>{t("page.mod.merge.classic_locked")}</AlertDescription>
                </Alert>
              )}

              <div className="grid gap-3">
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field>
                    <FieldLabel>{t("page.mod.merge.pack_name")}</FieldLabel>
                    <Input value={packName} onChange={(event) => setPackName(event.target.value)} />
                  </Field>
                  <div className="grid min-w-0 grid-cols-2 gap-3">
                    <KeyField
                      label={t("page.mod.merge.forward_key")}
                      value={plan.forwardKey}
                      onOpen={() => setEditingKey("forward")}
                    />
                    <KeyField
                      label={t("page.mod.merge.back_key")}
                      value={plan.backKey}
                      onOpen={() => setEditingKey("back")}
                    />
                  </div>
                </div>
                <div className="grid gap-3 sm:grid-cols-3">
                  <Field>
                    <FieldLabel>{t("page.mod.merge.placement")}</FieldLabel>
                    <div className="flex gap-2">
                      <Button
                        type="button"
                        size="sm"
                        variant={placement === "in_place" ? "default" : "outline"}
                        onClick={() => setPlacement("in_place")}
                      >
                        {t("page.mod.merge.placement_in_place")}
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant={placement === "new_folder" ? "default" : "outline"}
                        onClick={() => setPlacement("new_folder")}
                      >
                        {t("page.mod.merge.placement_new_folder")}
                      </Button>
                    </div>
                  </Field>
                  <Field>
                    <FieldLabel>{t("page.mod.merge.engine")}</FieldLabel>
                    <EngineToggle
                      engine={plan.engine}
                      onChange={(engine) =>
                        setPlan({ ...plan, engine, includeVanilla: engine === "namespace" })
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel>{t("page.mod.merge.include_vanilla")}</FieldLabel>
                    <label className="flex h-8 items-center gap-2 text-sm">
                      <Switch
                        checked={plan.includeVanilla}
                        disabled={plan.engine !== "namespace"}
                        onCheckedChange={(includeVanilla) => setPlan({ ...plan, includeVanilla })}
                      />
                    </label>
                  </Field>
                </div>
              </div>

              <MergeGroupEditor
                node={plan}
                packsByPath={packsByPath}
                onChange={setPlan}
                allowGrouping
                scrollable
              />
            </div>
          )}

          <div className="flex justify-end space-x-2">
            <Button variant="outline" onClick={() => setOpen(false)} disabled={isMerging}>
              {t("g.cancel")}
            </Button>
            <Button onClick={() => void handleMerge()} disabled={!canSubmit || isMerging}>
              {isMerging && <Loader2Icon className="animate-spin" />}
              {t("page.mod.merge.confirm")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog
        open={editingKey !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setEditingKey(null);
        }}
      >
        <DialogContent onClick={(event) => event.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>
              {editingKey === "back"
                ? t("page.mod.merge.back_key")
                : t("page.mod.merge.forward_key")}
            </DialogTitle>
          </DialogHeader>
          {plan && (
            <KeyRecorder
              key={editingKey ?? "empty"}
              defaultValue={editingKey === "back" ? plan.backKey : plan.forwardKey}
              otherKeys={[editingKey === "back" ? plan.forwardKey : plan.backKey].filter(Boolean)}
              onSave={(nextValue) => {
                setPlan(
                  editingKey === "back"
                    ? { ...plan, backKey: nextValue }
                    : { ...plan, forwardKey: nextValue },
                );
                setEditingKey(null);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function MergeGroupEditor({
  node,
  packsByPath,
  onChange,
  allowGrouping = false,
  scrollable = false,
}: {
  node: MergePlanGroup;
  packsByPath: Map<string, MergePackClassification>;
  onChange: (node: MergePlanGroup) => void;
  allowGrouping?: boolean;
  scrollable?: boolean;
}) {
  const { t } = useTranslation();
  const updateChild = (index: number, child: MergePlanNode) => {
    onChange({
      ...node,
      children: node.children.map((entry, entryIndex) => (entryIndex === index ? child : entry)),
    });
  };

  const items = (
    <div className="space-y-3">
      {node.children.map((child, index) => {
        const canGroup =
          allowGrouping && child.kind === "leaf" && node.children[index + 1]?.kind === "leaf";

        return (
          <div
            key={child.kind === "leaf" ? child.path : child.id}
            className="flex items-stretch gap-2"
          >
            <div className="flex flex-col justify-center gap-1">
              <Button
                type="button"
                size="icon-xs"
                variant="outline"
                disabled={index === 0}
                onClick={() =>
                  onChange({
                    ...node,
                    children: moveItem(node.children, index, index - 1),
                  })
                }
              >
                <ArrowUpIcon />
              </Button>
              <Button
                type="button"
                size="icon-xs"
                variant="outline"
                disabled={index === node.children.length - 1}
                onClick={() =>
                  onChange({
                    ...node,
                    children: moveItem(node.children, index, index + 1),
                  })
                }
              >
                <ArrowDownIcon />
              </Button>
              {canGroup && (
                <Button
                  type="button"
                  size="icon-xs"
                  variant="outline"
                  title={t("page.mod.merge.group_next")}
                  onClick={() =>
                    onChange({
                      ...node,
                      children: [
                        ...node.children.slice(0, index),
                        {
                          kind: "group",
                          id: `group-${Date.now()}-${index}`,
                          engine: node.engine,
                          name: `${node.name}Part${index + 1}`,
                          forwardKey: node.forwardKey,
                          backKey: node.backKey,
                          includeVanilla: false,
                          children: [child, node.children[index + 1]],
                        },
                        ...node.children.slice(index + 2),
                      ],
                    })
                  }
                >
                  <GroupIcon />
                </Button>
              )}
              {child.kind === "group" && (
                <Button
                  type="button"
                  size="icon-xs"
                  variant="outline"
                  title={t("page.mod.merge.ungroup")}
                  onClick={() =>
                    onChange({
                      ...node,
                      children: [
                        ...node.children.slice(0, index),
                        ...child.children,
                        ...node.children.slice(index + 1),
                      ],
                    })
                  }
                >
                  <UngroupIcon />
                </Button>
              )}
            </div>
            <div className="min-w-0 flex-1 self-stretch">
              {child.kind === "leaf" ? (
                <MergeLeafRow pack={packsByPath.get(child.path)} path={child.path} />
              ) : (
                <MergeGroupEditor
                  node={child}
                  packsByPath={packsByPath}
                  onChange={(next) => updateChild(index, next)}
                />
              )}
            </div>
          </div>
        );
      })}
    </div>
  );

  if (!scrollable) {
    return <div className="rounded-lg border p-3">{items}</div>;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
      <ScrollArea className="min-h-0 flex-1 overflow-y-auto" viewportClassName="p-3">
        {items}
      </ScrollArea>
    </div>
  );
}

function MergeLeafRow({ pack, path: packPath }: { pack?: MergePackClassification; path: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex h-full flex-col justify-center rounded-md border bg-background/60 px-3 py-2">
      <div className="truncate font-medium">{pack?.name ?? stripDisabledPrefix(packPath)}</div>
      <div className="text-xs text-muted-foreground">
        {t(`page.mod.merge.family.${pack?.family ?? "ordinary"}`)}
        {pack && !canUseClassic(pack) ? ` · ${t("page.mod.merge.namespace_only")}` : ""}
      </div>
    </div>
  );
}

function EngineToggle({
  engine,
  onChange,
}: {
  engine: MergeEngine;
  onChange: (engine: MergeEngine) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex gap-1">
      <Button
        type="button"
        size="sm"
        variant={engine === "classic" ? "default" : "outline"}
        onClick={() => onChange("classic")}
      >
        {t("page.mod.merge.engine_classic")}
      </Button>
      <Button
        type="button"
        size="sm"
        variant={engine === "namespace" ? "default" : "outline"}
        onClick={() => onChange("namespace")}
      >
        {t("page.mod.merge.engine_namespace")}
      </Button>
    </div>
  );
}

function KeyField({ label, value, onOpen }: { label: string; value: string; onOpen: () => void }) {
  const { t } = useTranslation();
  const labels = value
    .split(" ")
    .map((key) => formatKeyLabel(key))
    .filter((key): key is string => key !== null);

  return (
    <Field className="min-w-0">
      <FieldLabel>{label}</FieldLabel>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-8 w-full min-w-20 justify-start px-2"
        onClick={onOpen}
      >
        {labels.length > 0 ? (
          <span className="flex items-center gap-1">
            {labels.map((keyLabel) => (
              <Kbd key={keyLabel}>{keyLabel}</Kbd>
            ))}
          </span>
        ) : (
          <span className="text-muted-foreground">{t("page.mod.merge.press_key")}</span>
        )}
      </Button>
    </Field>
  );
}

function moveItem<T>(items: T[], from: number, to: number) {
  if (to < 0 || to >= items.length) return items;
  const next = [...items];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}
