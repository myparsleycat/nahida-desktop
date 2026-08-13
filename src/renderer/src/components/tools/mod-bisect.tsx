import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
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
import { useGames } from "@renderer/hooks/use-mod-data";
import { useSetting } from "@renderer/hooks/use-settings";
import { setSetting } from "@renderer/lib/settings";
import { disabledPrefixString, isNteImporter } from "@shared/mod";
import type { BisectSnapshot, GameConfig } from "@shared/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PlusIcon, XIcon } from "lucide-react";
import { useEffect, useImperativeHandle, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ScrollArea } from "../ui/scroll-area";

function relativeDisplay(rootPath: string | null, iniPath: string): string {
  if (!rootPath) return iniPath;
  const prefix = rootPath.replace(/[\\/]+$/, "");
  if (iniPath.toLowerCase().startsWith(prefix.toLowerCase())) {
    const relative = iniPath.slice(prefix.length);
    return relative.replace(/^[\\/]+/, "");
  }
  return iniPath;
}

function statusColor(status: BisectSnapshot["status"]) {
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

function isExcludeValidationMessage(message: string) {
  return (
    message.includes("Exclude path is empty.") ||
    message.includes("Exclude path must be inside the selected importer.") ||
    message.includes("Exclude path cannot be the importer root.") ||
    message.includes("Exclude path does not exist.")
  );
}

const ALL_EXCLUDED_ERROR = "All enabled INIs were excluded";

type ExcludeRow = {
  id: string;
  value: string;
  committed: string | null;
};

function createExcludeRow(): ExcludeRow {
  return { id: crypto.randomUUID(), value: "", committed: null };
}

export default function ModBisect() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [selectedGame, setSelectedGame] = useState<string>("");
  const [starting, setStarting] = useState(false);
  const startingRef = useRef(false);
  const excludeListRef = useRef<{ flush: () => Promise<string[] | null> }>(null);
  const { data: preserveD3dx = true } = useSetting("general.bisectPreserveD3dx");

  const { data: games = [] } = useGames();

  const { data: snapshot } = useQuery<BisectSnapshot | null>({
    queryKey: ["tools:bisectState"],
    queryFn: () => window.api.invoke("tools:bisectGetState"),
    refetchInterval: (query) => {
      const value = query.state.data;
      if (!value) return false;
      if (value.status === "scanning" || value.status === "reverting") return 500;
      return false;
    },
  });

  useEffect(() => {
    const unsubscribe = window.api.on("tools:bisectState", (next) => {
      queryClient.setQueryData(["tools:bisectState"], next);
    });
    return unsubscribe;
  }, [queryClient]);

  useEffect(() => {
    if (!selectedGame && games.length > 0) {
      const first = games.find((g) => !isNteImporter(g.importer));
      if (first) setSelectedGame(first.game);
    }
  }, [games, selectedGame]);

  const startMutation = useMutation({
    mutationFn: ({ game, excludePaths }: { game: string; excludePaths: string[] }) =>
      window.api.invoke("tools:bisectStart", game, excludePaths),
    onError: (err) => {
      if (isExcludeValidationMessage(err.message)) {
        toast.warning(t("page.tools.mod_bisect.exclude_invalid"));
      } else {
        toast.error(err.message);
      }
      void queryClient.invalidateQueries({ queryKey: ["tools:bisectState"] });
    },
  });

  const respondMutation = useMutation({
    mutationFn: (fixed: boolean) => window.api.invoke("tools:bisectRespond", fixed),
    onError: (err) => {
      toast.error(err.message);
      void queryClient.invalidateQueries({ queryKey: ["tools:bisectState"] });
    },
  });

  const undoMutation = useMutation({
    mutationFn: () => window.api.invoke("tools:bisectUndoLastRound"),
    onError: (err) => toast.error(err.message),
  });

  const cancelMutation = useMutation({
    mutationFn: () => window.api.invoke("tools:bisectCancel"),
    onError: (err) => toast.error(err.message),
  });

  const finalizeMutation = useMutation({
    mutationFn: (keepDisabled: string[]) => window.api.invoke("tools:bisectFinalize", keepDisabled),
    onError: (err) => toast.error(err.message),
  });

  const recoverMutation = useMutation({
    mutationFn: (game: string) => window.api.invoke("tools:bisectRecover", game),
    onSuccess: (count) => {
      if (count > 0) {
        toast.success(t("page.tools.mod_bisect.recover_disabled_success", { count }));
      } else {
        toast.info(t("page.tools.mod_bisect.recover_disabled_none"));
      }
    },
    onError: (err) => toast.error(err.message),
  });

  const status = snapshot?.status ?? "idle";
  const isActive = status === "scanning" || status === "round";
  const canStart = !!selectedGame && !isActive && status !== "reverting";
  const canRecover =
    !!selectedGame && (status === "idle" || status === "cancelled") && !recoverMutation.isPending;
  const isBusy =
    starting ||
    startMutation.isPending ||
    respondMutation.isPending ||
    undoMutation.isPending ||
    cancelMutation.isPending ||
    finalizeMutation.isPending ||
    recoverMutation.isPending;

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      <Card>
        <CardHeader>
          <CardTitle>{t("page.tools.mod_bisect.title")}</CardTitle>
          <CardDescription>{t("page.tools.mod_bisect.description")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <GameSelect
              games={games}
              value={selectedGame}
              onChange={setSelectedGame}
              disabled={isActive || isBusy}
            />
            <Button
              onClick={() => {
                if (!selectedGame || startingRef.current) return;
                const game = selectedGame;
                startingRef.current = true;
                setStarting(true);
                void (async () => {
                  try {
                    const excludePaths = await excludeListRef.current?.flush();
                    if (excludePaths === null) return;
                    await startMutation.mutateAsync({
                      game,
                      excludePaths: excludePaths ?? [],
                    });
                  } catch {
                    // startMutation.onError already reported the failure
                  } finally {
                    startingRef.current = false;
                    setStarting(false);
                  }
                })();
              }}
              disabled={!canStart || isBusy}
            >
              {t("page.tools.mod_bisect.start")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => cancelMutation.mutate()}
              disabled={
                !snapshot ||
                status === "idle" ||
                status === "done" ||
                status === "cancelled" ||
                isBusy
              }
            >
              {t("page.tools.mod_bisect.cancel")}
            </Button>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="secondary"
                    onClick={() => selectedGame && recoverMutation.mutate(selectedGame)}
                    disabled={!canRecover || isBusy}
                  />
                }
              >
                {t("page.tools.mod_bisect.recover_disabled")}
              </TooltipTrigger>
              <TooltipContent>{t("page.tools.mod_bisect.recover_disabled_hint")}</TooltipContent>
            </Tooltip>
            <span className={`font-mono text-xs ${statusColor(status)}`}>
              {t(`page.tools.mod_bisect.status.${status}`)}
            </span>
          </div>

          <ExcludePathList
            ref={excludeListRef}
            game={selectedGame}
            disabled={isActive || isBusy || !selectedGame}
          />

          <div className="flex items-center justify-between gap-2 rounded-md border p-2">
            <div className="flex flex-col gap-0.5">
              <span className="text-sm font-medium">
                {t("page.tools.mod_bisect.preserve_d3dx")}
              </span>
              <span className="text-xs text-muted-foreground">
                {t("page.tools.mod_bisect.preserve_d3dx_description")}
              </span>
            </div>
            <Switch
              checked={preserveD3dx}
              onCheckedChange={(checked) => void setSetting("general.bisectPreserveD3dx", checked)}
              disabled={isActive || isBusy}
            />
          </div>

          {snapshot?.error && status !== "done" ? (
            <div className="rounded-md border border-destructive/20 bg-destructive/10 p-2 text-xs text-destructive">
              {snapshot.error}
            </div>
          ) : null}
        </CardContent>
      </Card>

      {snapshot && status === "round" ? (
        <RoundView
          snapshot={snapshot}
          busy={isBusy}
          onRespond={(fixed) => respondMutation.mutate(fixed)}
          onUndo={() => undoMutation.mutate()}
        />
      ) : null}

      {snapshot && status === "scanning" ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.scanning")}
          </CardContent>
        </Card>
      ) : null}

      {snapshot && status === "done" && snapshot.finalBadPath ? (
        <DoneView
          snapshot={snapshot}
          busy={isBusy}
          onFinalize={(keepDisabled) => finalizeMutation.mutate(keepDisabled)}
        />
      ) : null}

      {snapshot &&
      status === "done" &&
      !snapshot.finalBadPath &&
      snapshot.error === ALL_EXCLUDED_ERROR ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.all_excluded")}
          </CardContent>
        </Card>
      ) : null}

      {snapshot &&
      status === "done" &&
      !snapshot.finalBadPath &&
      snapshot.error &&
      snapshot.error !== ALL_EXCLUDED_ERROR ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.bisect_inconclusive")}
          </CardContent>
        </Card>
      ) : null}

      {snapshot && status === "done" && !snapshot.finalBadPath && !snapshot.error ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.no_inis_found")}
          </CardContent>
        </Card>
      ) : null}

      {snapshot && status === "cancelled" ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.cancelled")}
          </CardContent>
        </Card>
      ) : null}

      {snapshot && status === "reverting" ? (
        <Card>
          <CardContent className="text-sm text-muted-foreground">
            {t("page.tools.mod_bisect.reverting")}
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

function ExcludePathList({
  ref,
  game,
  disabled,
}: {
  ref?: React.Ref<{ flush: () => Promise<string[] | null> }>;
  game: string;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<ExcludeRow[]>([]);
  const rowsRef = useRef(rows);
  const committingRef = useRef(new Map<string, Promise<boolean>>());
  const lastFailedRef = useRef(new Map<string, string>());

  useEffect(() => {
    rowsRef.current = rows;
  }, [rows]);

  useEffect(() => {
    rowsRef.current = [];
    setRows([]);
  }, [game]);

  const commitRow = (id: string): Promise<boolean> => {
    const inFlight = committingRef.current.get(id);
    if (inFlight) return inFlight;

    const promise = (async () => {
      const row = rowsRef.current.find((entry) => entry.id === id);
      if (!row) return true;

      const trimmed = row.value.trim();
      if (!trimmed) {
        lastFailedRef.current.delete(id);
        const next = rowsRef.current.filter((entry) => entry.id !== id);
        rowsRef.current = next;
        setRows(next);
        return true;
      }
      if (row.committed !== null && row.value === row.committed) return true;
      if (lastFailedRef.current.get(id) === trimmed) return false;
      if (!game) {
        toast.warning(t("page.tools.mod_bisect.exclude_no_game"));
        return false;
      }
      try {
        const relative = await window.api.invoke("tools:bisectValidateExcludePath", game, trimmed);
        if (
          rowsRef.current.some(
            (entry) => entry.id !== id && entry.committed?.toLowerCase() === relative.toLowerCase(),
          )
        ) {
          lastFailedRef.current.set(id, trimmed);
          toast.warning(t("page.tools.mod_bisect.exclude_duplicate"));
          return false;
        }
        lastFailedRef.current.delete(id);
        const next = rowsRef.current.map((entry) =>
          entry.id === id ? { ...entry, value: relative, committed: relative } : entry,
        );
        rowsRef.current = next;
        setRows(next);
        return true;
      } catch {
        lastFailedRef.current.set(id, trimmed);
        toast.warning(t("page.tools.mod_bisect.exclude_invalid"));
        return false;
      }
    })();

    committingRef.current.set(id, promise);
    void promise.finally(() => {
      if (committingRef.current.get(id) === promise) committingRef.current.delete(id);
    });
    return promise;
  };

  useImperativeHandle(ref, () => ({
    flush: async () => {
      const ids = rowsRef.current.map((entry) => entry.id);
      const committed: string[] = [];
      const ok = await ids.reduce(async (previous, id) => {
        if (!(await previous)) return false;
        const existing = rowsRef.current.find((entry) => entry.id === id)?.committed;
        if (!(await commitRow(id))) return false;
        const next = rowsRef.current.find((entry) => entry.id === id)?.committed ?? existing;
        if (next) committed.push(next);
        return true;
      }, Promise.resolve(true));
      if (!ok) return null;
      return committed;
    },
  }));

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-0.5">
        <span className="text-sm font-medium">{t("page.tools.mod_bisect.exclude_paths")}</span>
        <span className="text-xs text-muted-foreground">
          {t("page.tools.mod_bisect.exclude_paths_description")}
        </span>
      </div>
      <div className="flex flex-col gap-1.5">
        {rows.map((row) => (
          <div key={row.id} className="flex items-center gap-1.5">
            <Input
              className="flex-1"
              value={row.value}
              disabled={disabled}
              placeholder={t("page.tools.mod_bisect.exclude_paths_placeholder")}
              onChange={(event) => {
                lastFailedRef.current.delete(row.id);
                const next = rowsRef.current.map((entry) =>
                  entry.id === row.id ? { ...entry, value: event.target.value } : entry,
                );
                rowsRef.current = next;
                setRows(next);
              }}
              onBlur={(event) => {
                if (
                  event.relatedTarget instanceof HTMLElement &&
                  event.relatedTarget.closest(`[data-exclude-delete="${row.id}"]`)
                ) {
                  return;
                }
                void commitRow(row.id);
              }}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return;
                if (event.key === "Enter") event.currentTarget.blur();
              }}
            />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-7 shrink-0"
              disabled={disabled}
              data-exclude-delete={row.id}
              aria-label={t("page.tools.mod_bisect.exclude_paths_remove")}
              onClick={() => {
                const next = rowsRef.current.filter((entry) => entry.id !== row.id);
                rowsRef.current = next;
                setRows(next);
              }}
            >
              <XIcon className="size-3.5" />
            </Button>
          </div>
        ))}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          disabled={disabled}
          onClick={() => {
            const next = [...rowsRef.current, createExcludeRow()];
            rowsRef.current = next;
            setRows(next);
          }}
        >
          <PlusIcon className="size-3.5" />
          {t("page.tools.mod_bisect.exclude_paths_add")}
        </Button>
      </div>
    </div>
  );
}

function GameSelect({
  games,
  value,
  onChange,
  disabled,
}: {
  games: GameConfig[];
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  if (games.length === 0) {
    return (
      <Select disabled>
        <SelectTrigger className="min-w-56">
          <SelectValue placeholder={t("page.tools.mod_bisect.no_games")} />
        </SelectTrigger>
        <SelectGroup>
          <SelectContent />
        </SelectGroup>
      </Select>
    );
  }

  return (
    <Select
      value={value}
      onValueChange={(v) => {
        if (v === null) return;
        onChange(v);
      }}
      disabled={disabled}
    >
      <SelectTrigger className="min-w-56">
        <SelectValue placeholder={t("page.tools.mod_bisect.select_game")} />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {games.map((game) => {
            const nte = isNteImporter(game.importer);
            const item = (
              <SelectItem key={game.game} value={game.game} disabled={nte}>
                {game.game}
                {nte ? ` (${t("page.tools.mod_bisect.nte_unsupported")})` : ""}
              </SelectItem>
            );
            if (!nte) return item;
            return (
              <Tooltip key={game.game}>
                <TooltipTrigger>{item}</TooltipTrigger>
                <TooltipContent>{t("page.tools.mod_bisect.nte_tooltip")}</TooltipContent>
              </Tooltip>
            );
          })}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

function RoundView({
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

function DoneView({
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
