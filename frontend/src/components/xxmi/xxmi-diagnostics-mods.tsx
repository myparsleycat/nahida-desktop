import { XXMI } from "@bindings/xxmi";
import type { DiagnosticsMod } from "@bindings/xxmi/models";
import { Badge } from "@renderer/components/ui/badge";
import { Button } from "@renderer/components/ui/button";
import { Checkbox } from "@renderer/components/ui/checkbox";
import { cn } from "@renderer/lib/utils";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { ChevronRightIcon, Loader2Icon, XIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

type Tree = {
  importer: string;
  selected: ReadonlyMap<string, DiagnosticsMod>;
  expanded: ReadonlySet<string>;
  disabled: boolean;
  toggleExpanded: (relative: string) => void;
  toggleSelected: (mod: DiagnosticsMod, checked: boolean) => void;
};

/** Picks folders at any depth of the importer's Mods folder, keyed by their path below it. */
export function DiagnosticsModPicker({
  importer,
  selected,
  disabled,
  onSelectedChange,
}: {
  importer: string;
  selected: ReadonlyMap<string, DiagnosticsMod>;
  disabled: boolean;
  onSelectedChange: (selected: Map<string, DiagnosticsMod>) => void;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());

  const tree: Tree = {
    importer,
    selected,
    expanded,
    disabled,
    toggleExpanded: (relative) =>
      setExpanded((current) => {
        const next = new Set(current);
        if (!next.delete(relative)) next.add(relative);
        return next;
      }),
    toggleSelected: (mod, checked) => {
      // A selected folder is sent whole, so the folders it carries leave the selection.
      const next = new Map([...selected].filter(([, other]) => !carries(mod.relative, other)));
      if (checked) next.set(mod.relative, mod);
      else next.delete(mod.relative);
      onSelectedChange(next);
    },
  };

  return (
    <div className="space-y-2">
      <div className="max-h-56 overflow-y-auto rounded-md border p-1">
        <ModFolderList tree={tree} parent="" depth={0} />
      </div>
      {selected.size > 0 ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.diagnostics.modsSelected", { count: selected.size })}
          </span>
          {[...selected.values()].map((mod) => (
            <span
              key={mod.relative}
              className="flex max-w-full items-center gap-0.5 rounded-md bg-muted/50 py-0.5 pr-0.5 pl-2 text-xs"
            >
              <span className="min-w-0 truncate" title={mod.path}>
                {mod.relative}
              </span>
              <Button
                variant="ghost"
                size="icon-xs"
                disabled={disabled}
                aria-label={t("page.setting.feedback.attachments.remove")}
                onClick={() => tree.toggleSelected(mod, false)}
              >
                <XIcon />
              </Button>
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ModFolderList({ tree, parent, depth }: { tree: Tree; parent: string; depth: number }) {
  const { t } = useTranslation();
  const { data, error, isPending } = useQuery({
    queryKey: ["xxmi:diagnostics-mods", tree.importer, parent],
    queryFn: async () => (await XXMI.ListDiagnosticsMods(tree.importer, parent)) ?? [],
  });
  const indent = { paddingLeft: `${depth * 16 + 8}px` };

  if (isPending) {
    return (
      <p className="py-1 text-muted-foreground" style={indent}>
        <Loader2Icon className="size-3.5 animate-spin" />
      </p>
    );
  }
  if (error) {
    return (
      <p className="py-1 text-xs text-destructive" style={indent}>
        {toErrorMessage(error)}
      </p>
    );
  }
  if (data.length === 0 && depth === 0) {
    return (
      <p className="px-2 py-1 text-xs text-muted-foreground">
        {t("page.setting.xxmi.diagnostics.noMods")}
      </p>
    );
  }
  return (
    <ul>
      {data.map((mod) => (
        <ModFolderRow key={mod.relative} tree={tree} mod={mod} depth={depth} />
      ))}
    </ul>
  );
}

function ModFolderRow({ tree, mod, depth }: { tree: Tree; mod: DiagnosticsMod; depth: number }) {
  const { t } = useTranslation();
  const open = tree.expanded.has(mod.relative);
  const keys = [...tree.selected.keys()];
  const checked = tree.selected.has(mod.relative);
  const covered = keys.some((key) => carries(key, mod));
  const partial = keys.some((key) => key.startsWith(`${mod.relative}/`));

  return (
    <li>
      <div
        className="flex items-center gap-1 rounded-md pr-2 hover:bg-muted/50"
        style={{ paddingLeft: `${depth * 16}px` }}
      >
        {mod.hasChildren ? (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-expanded={open}
            aria-label={t(
              open
                ? "page.setting.xxmi.diagnostics.collapseMod"
                : "page.setting.xxmi.diagnostics.expandMod",
              { name: mod.name },
            )}
            onClick={() => tree.toggleExpanded(mod.relative)}
          >
            <ChevronRightIcon className={cn("transition-transform", open && "rotate-90")} />
          </Button>
        ) : (
          <span className="size-6 shrink-0" />
        )}
        <label className="flex min-w-0 flex-1 items-center gap-2 py-1 text-sm">
          <Checkbox
            checked={checked || covered}
            indeterminate={!checked && !covered && partial}
            disabled={tree.disabled || covered}
            onCheckedChange={(value) => tree.toggleSelected(mod, value)}
          />
          <span className="min-w-0 flex-1 truncate" title={mod.path}>
            {mod.name}
          </span>
          {mod.linkedAt === mod.relative ? (
            <Badge variant="outline" title={t("page.setting.xxmi.diagnostics.modLinkedHint")}>
              {t("page.setting.xxmi.diagnostics.modLinked")}
            </Badge>
          ) : null}
          {mod.disabled ? (
            <Badge variant="outline">{t("page.setting.xxmi.diagnostics.modDisabled")}</Badge>
          ) : null}
        </label>
      </div>
      {open ? <ModFolderList tree={tree} parent={mod.relative} depth={depth + 1} /> : null}
    </li>
  );
}

// A report walks past a link inside the folder it attaches, so an ancestor carries a folder only from the
// folder's deepest link down.
function carries(ancestor: string, mod: DiagnosticsMod) {
  return (
    mod.relative.startsWith(`${ancestor}/`) &&
    (mod.linkedAt === "" || ancestor === mod.linkedAt || ancestor.startsWith(`${mod.linkedAt}/`))
  );
}
