import { Tools } from "@bindings/tools";
import {
  OutlineFileState,
  type OutlineApplyInput,
  type OutlineFile,
  type OutlineInspection,
} from "@bindings/tools/efmi_outline";
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
import { Checkbox } from "@renderer/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { toErrorMessage } from "@shared/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { Loader2Icon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface EfmiOutlineDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  modPath: string;
  modName: string;
}

type Selection = Record<string, string[]>;

interface FileTarget {
  modPath: string;
  path: string;
}

export function EfmiOutlineDialog({
  open,
  onOpenChange,
  modPath,
  modName,
}: EfmiOutlineDialogProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const inspection = useQuery({
    queryKey: outlineQueryKey(modPath),
    queryFn: () => Tools.EFMIOutlineInspect(modPath),
    enabled: open && modPath !== "",
    gcTime: 0,
    retry: false,
  });
  // The draft belongs to the inspection it was made from, so a refreshed
  // inspection starts again from what the files actually contain.
  const [draft, setDraft] = useState<{ source: OutlineInspection; selection: Selection } | null>(
    null,
  );
  const [adoptPath, setAdoptPath] = useState<string | null>(null);

  const data = inspection.data;
  const files = data?.files ?? [];
  const applied = appliedSelection(files);
  const selection = draft && draft.source === data ? draft.selection : applied;
  const editable = files.filter(isEditable);
  const dirty = editable.some((file) => !sameIds(selection[file.path], applied[file.path]));

  // The dialog stays mounted while another mod is opened, so a request settles
  // into the cache of the mod it was made for, never the one shown now.
  const onSuccess = (target: string, result: OutlineInspection, message: string) => {
    queryClient.setQueryData(outlineQueryKey(target), result);
    toast.success(message);
  };
  const onError = (target: string, error: unknown) => {
    toast.error(outlineErrorMessage(t, error));
    void queryClient.invalidateQueries({ queryKey: outlineQueryKey(target) });
  };
  const applyMutation = useMutation({
    mutationFn: (input: OutlineApplyInput) => Tools.EFMIOutlineApply(input),
    onSuccess: (result, input) =>
      onSuccess(input.modPath, result, t("page.tools.efmi_outline.applied")),
    onError: (error, input) => onError(input.modPath, error),
  });
  const revertMutation = useMutation({
    mutationFn: (target: FileTarget) => Tools.EFMIOutlineRevert(target.modPath, target.path),
    onSuccess: (result, target) =>
      onSuccess(target.modPath, result, t("page.tools.efmi_outline.reverted")),
    onError: (error, target) => onError(target.modPath, error),
  });
  const adoptMutation = useMutation({
    mutationFn: (target: FileTarget) => Tools.EFMIOutlineAdopt(target.modPath, target.path),
    onSuccess: (result, target) =>
      onSuccess(target.modPath, result, t("page.tools.efmi_outline.adopted")),
    onError: (error, target) => onError(target.modPath, error),
  });
  const busy = applyMutation.isPending || revertMutation.isPending || adoptMutation.isPending;

  const toggle = (file: OutlineFile, id: string, checked: boolean) => {
    if (!data) return;
    const current = selection[file.path] ?? [];
    const next = checked ? [...current, id] : current.filter((value) => value !== id);
    setDraft({ source: data, selection: { ...selection, [file.path]: next } });
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          className="flex max-h-[85vh] flex-col gap-0 overflow-hidden sm:max-w-2xl"
          onClick={(event) => event.stopPropagation()}
        >
          <DialogHeader className="shrink-0 pr-8 pb-4">
            <DialogTitle>{t("page.tools.efmi_outline.title")}</DialogTitle>
            <p className="truncate text-sm text-muted-foreground" title={modName}>
              {modName}
            </p>
            <DialogDescription>{t("page.tools.efmi_outline.description")}</DialogDescription>
          </DialogHeader>

          <div className="-mx-4 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4 *:shrink-0">
            {inspection.isLoading && (
              <Loader2Icon className="mx-auto size-5 animate-spin text-muted-foreground" />
            )}
            {inspection.error && (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertDescription>{outlineErrorMessage(t, inspection.error)}</AlertDescription>
              </Alert>
            )}
            {data && files.length === 0 && (
              <p className="text-sm">{t("page.tools.efmi_outline.empty")}</p>
            )}
            {files.map((file) => (
              <OutlineFileCard
                key={file.path}
                file={file}
                selected={selection[file.path] ?? []}
                disabled={busy}
                onToggle={(id, checked) => toggle(file, id, checked)}
                onRevert={() => revertMutation.mutate({ modPath, path: file.path })}
                onAdopt={() => setAdoptPath(file.path)}
              />
            ))}
          </div>

          <DialogFooter className="shrink-0">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              {t("g.close")}
            </Button>
            <Button
              disabled={!dirty || busy}
              onClick={() =>
                applyMutation.mutate({
                  modPath,
                  files: editable.map((file) => ({
                    path: file.path,
                    partIds: selection[file.path] ?? [],
                  })),
                })
              }
            >
              {applyMutation.isPending && <Loader2Icon className="animate-spin" />}
              {t("g.apply")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={adoptPath !== null} onOpenChange={(next) => !next && setAdoptPath(null)}>
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("page.tools.efmi_outline.modified.confirm_title")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.tools.efmi_outline.modified.confirm_description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (adoptPath) adoptMutation.mutate({ modPath, path: adoptPath });
                setAdoptPath(null);
              }}
            >
              {t("page.tools.efmi_outline.modified.adopt")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

interface OutlineFileCardProps {
  file: OutlineFile;
  selected: string[];
  disabled: boolean;
  onToggle: (id: string, checked: boolean) => void;
  onRevert: () => void;
  onAdopt: () => void;
}

function OutlineFileCard({
  file,
  selected,
  disabled,
  onToggle,
  onRevert,
  onAdopt,
}: OutlineFileCardProps) {
  const { t } = useTranslation();
  const editable = isEditable(file);
  const manual = file.manual ?? [];

  return (
    <section className="space-y-1">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium" title={file.path}>
          {file.path}
        </span>
        <Badge variant={stateBadgeVariant(file.state)}>
          {t(`page.tools.efmi_outline.state.${file.state}`)}
        </Badge>
        {file.hasBackup && editable && (
          <Button size="sm" variant="outline" disabled={disabled} onClick={onRevert}>
            {t("page.tools.efmi_outline.revert")}
          </Button>
        )}
      </div>

      {file.state === OutlineFileState.StateModified && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>{t("page.tools.efmi_outline.modified.title")}</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>{t("page.tools.efmi_outline.modified.description")}</p>
            <Button size="sm" variant="outline" disabled={disabled} onClick={onAdopt}>
              {t("page.tools.efmi_outline.modified.adopt")}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {(file.parts ?? []).map((part) => (
        <label
          key={part.id}
          className="flex w-full items-center gap-2 rounded-md px-3 py-2 transition-colors hover:bg-muted/60"
        >
          <Checkbox
            checked={selected.includes(part.id)}
            disabled={disabled || !editable || part.locked}
            onCheckedChange={(checked) => onToggle(part.id, checked)}
          />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium" title={part.label}>
              {part.label}
            </span>
            <span className="mt-0.5 block truncate text-xs text-muted-foreground">
              {(part.sections ?? []).join(", ")} ·{" "}
              {t("page.tools.efmi_outline.lines", { lines: (part.lines ?? []).join(", ") })}
            </span>
            {part.locked && (
              <span className="mt-0.5 block text-xs text-muted-foreground">
                {t("page.tools.efmi_outline.locked")}
              </span>
            )}
          </span>
        </label>
      ))}

      {manual.length > 0 && (
        <details className="px-3 text-sm">
          <summary className="cursor-pointer text-muted-foreground">
            {t("page.tools.efmi_outline.manual.title", { count: manual.length })}
          </summary>
          <ul className="mt-2 space-y-1">
            {manual.map((item) => (
              <li key={`${item.section}:${item.line}`} className="text-xs break-all">
                <span className="font-mono">
                  [{item.section}] {item.line}: {item.text}
                </span>
                <span className="text-muted-foreground">
                  {" "}
                  · {t(`page.tools.efmi_outline.manual.${item.reason}`)}
                </span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

function outlineQueryKey(modPath: string) {
  return ["tools:efmiOutline", modPath];
}

function isEditable(file: OutlineFile) {
  return (
    file.state === OutlineFileState.StateOriginal || file.state === OutlineFileState.StateApplied
  );
}

function appliedSelection(files: OutlineFile[]): Selection {
  return Object.fromEntries(
    files.map((file) => [
      file.path,
      (file.parts ?? []).filter((part) => part.applied).map((part) => part.id),
    ]),
  );
}

function sameIds(left: string[] = [], right: string[] = []) {
  return left.length === right.length && left.every((id) => right.includes(id));
}

function stateBadgeVariant(state: OutlineFileState) {
  switch (state) {
    case OutlineFileState.StateApplied:
      return "default";
    case OutlineFileState.StateModified:
    case OutlineFileState.StateUnsupported:
      return "destructive";
    default:
      return "secondary";
  }
}

const errorCodePattern = /EFMI_OUTLINE_([A-Z_]+):([^\n]*)/;

function outlineErrorMessage(t: TFunction, error: unknown) {
  const message = toErrorMessage(error);
  const match = errorCodePattern.exec(message);
  const file = match?.[2].trim() ?? "";
  switch (match?.[1]) {
    case "NOT_EFMI":
      return t("page.tools.efmi_outline.error.not_efmi");
    case "MODIFIED":
      return t("page.tools.efmi_outline.error.modified", { file });
    case "STALE":
      return t("page.tools.efmi_outline.error.stale");
    case "UNSUPPORTED":
      return t("page.tools.efmi_outline.error.unsupported", { file });
    default:
      return message;
  }
}
