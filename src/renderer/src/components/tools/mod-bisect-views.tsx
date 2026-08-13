import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import { useSetting } from "@renderer/hooks/use-settings";
import { disabledPrefixString } from "@shared/mod";
import type { BisectSnapshot } from "@shared/types";
import { useTranslation } from "react-i18next";

import { ScrollArea } from "../ui/scroll-area";

const ALL_EXCLUDED_ERROR = "All enabled INIs were excluded";

export function relativeDisplay(rootPath: string | null, iniPath: string): string {
  if (!rootPath) return iniPath;
  const prefix = rootPath.replace(/[\\/]+$/, "");
  if (iniPath.toLowerCase().startsWith(prefix.toLowerCase())) {
    const relative = iniPath.slice(prefix.length);
    return relative.replace(/^[\\/]+/, "");
  }
  return iniPath;
}

export function statusColor(status: BisectSnapshot["status"]) {
  switch (status) {
    case "round":
      return "text-amber-600 dark:text-amber-400";
    case "done":
      return "text-green-600 dark:text-green-400";
    case "cancelled":
      return "text-muted-foreground";
    case "reverting":
      return "text-blue-600 dark:text-blue-400";
    case "scanning":
      return "text-blue-600 dark:text-blue-400";
    default:
      return "text-muted-foreground";
  }
}

export function isExcludeValidationMessage(message: string) {
  return (
    message.includes("Exclude path is empty.") ||
    message.includes("Exclude path must be inside the selected importer.") ||
    message.includes("Exclude path cannot be the importer root.") ||
    message.includes("Exclude path does not exist.")
  );
}

function StatusCard({ text }: { text: string }) {
  return (
    <Card>
      <CardContent className="text-sm text-muted-foreground">{text}</CardContent>
    </Card>
  );
}

export function RoundView({
  snapshot,
  busy,
  onRespond,
  onUndo,
}: {
  snapshot: BisectSnapshot;
  busy: boolean;
  onRespond: (fixed: boolean) => void;
  onUndo: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t("page.tools.mod_bisect.round_title", {
            round: snapshot.round,
            batchSize: snapshot.batchSize,
            remaining: snapshot.candidates.length,
          })}
        </CardTitle>
        <CardDescription>{t("page.tools.mod_bisect.round_description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="text-xs text-muted-foreground">
          {t("page.tools.mod_bisect.disabled_count", {
            count: snapshot.currentBatch.length,
          })}
        </div>
        {snapshot.excludePaths.length > 0 ? (
          <div className="text-xs text-muted-foreground">
            {t("page.tools.mod_bisect.exclude_count", {
              count: snapshot.excludePaths.length,
            })}
          </div>
        ) : null}
        <ScrollArea className="h-56 rounded border bg-muted/30 p-2">
          <ul className="space-y-0.5 font-mono text-xs break-all">
            {snapshot.currentBatch.map((iniPath) => (
              <li key={iniPath}>{relativeDisplay(snapshot.modRootPath, iniPath)}</li>
            ))}
          </ul>
        </ScrollArea>

        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={onUndo}
            disabled={busy || snapshot.undoStackDepth === 0}
          >
            {t("page.tools.mod_bisect.undo")}
          </Button>
          <Button variant="destructive" onClick={() => onRespond(false)} disabled={busy}>
            {t("page.tools.mod_bisect.still_broken")}
          </Button>
          <Button onClick={() => onRespond(true)} disabled={busy}>
            {t("page.tools.mod_bisect.problem_fixed")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

export function DoneView({
  snapshot,
  busy,
  onFinalize,
}: {
  snapshot: BisectSnapshot;
  busy: boolean;
  onFinalize: (keepDisabled: string[]) => void;
}) {
  const { t } = useTranslation();
  const { data: disabledPrefixStyle = "space" } = useSetting("mod.disabledPrefixStyle");
  const originalPath = snapshot.finalBadPath ?? "";
  const basename = originalPath ? (originalPath.split(/[\\/]+/).pop() ?? originalPath) : "";
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("page.tools.mod_bisect.done_title")}</CardTitle>
        <CardDescription>
          {t("page.tools.mod_bisect.done_description", {
            path: relativeDisplay(snapshot.modRootPath, originalPath),
          })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="text-xs text-muted-foreground">
          {t("page.tools.mod_bisect.keep_disabled_hint", {
            from: basename,
            to: `${disabledPrefixString(disabledPrefixStyle)}${basename}`,
          })}
        </div>
        <div className="text-xs text-muted-foreground">
          {t("page.tools.mod_bisect.new_bisect_hint")}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => onFinalize(snapshot.finalBadPath ? [snapshot.finalBadPath] : [])}
            disabled={busy}
          >
            {t("page.tools.mod_bisect.keep_disabled")}
          </Button>
          <Button onClick={() => onFinalize([])} disabled={busy}>
            {t("page.tools.mod_bisect.re_enable_all")}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

export function BisectStatusCards({
  snapshot,
  busy,
  onRespond,
  onUndo,
  onFinalize,
}: {
  snapshot: BisectSnapshot | null | undefined;
  busy: boolean;
  onRespond: (fixed: boolean) => void;
  onUndo: () => void;
  onFinalize: (keepDisabled: string[]) => void;
}) {
  const { t } = useTranslation();
  if (!snapshot) return null;

  const { status } = snapshot;
  if (status === "round") {
    return <RoundView snapshot={snapshot} busy={busy} onRespond={onRespond} onUndo={onUndo} />;
  }
  if (status === "scanning") {
    return <StatusCard text={t("page.tools.mod_bisect.scanning")} />;
  }
  if (status === "done" && snapshot.finalBadPath) {
    return <DoneView snapshot={snapshot} busy={busy} onFinalize={onFinalize} />;
  }
  if (status === "done" && !snapshot.finalBadPath && snapshot.error === ALL_EXCLUDED_ERROR) {
    return <StatusCard text={t("page.tools.mod_bisect.all_excluded")} />;
  }
  if (status === "done" && !snapshot.finalBadPath && snapshot.error) {
    return <StatusCard text={t("page.tools.mod_bisect.bisect_inconclusive")} />;
  }
  if (status === "done" && !snapshot.finalBadPath && !snapshot.error) {
    return <StatusCard text={t("page.tools.mod_bisect.no_inis_found")} />;
  }
  if (status === "cancelled") {
    return <StatusCard text={t("page.tools.mod_bisect.cancelled")} />;
  }
  if (status === "reverting") {
    return <StatusCard text={t("page.tools.mod_bisect.reverting")} />;
  }
  return null;
}
