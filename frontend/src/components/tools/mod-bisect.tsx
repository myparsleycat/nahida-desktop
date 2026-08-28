import { Tools } from "@bindings/tools";
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
import { isNteImporter } from "@shared/mod";
import type { BisectSnapshot, GameConfig } from "@shared/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { PlusIcon, XIcon } from "lucide-react";
import { useEffect, useImperativeHandle, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { BisectStatusCards, isExcludeValidationMessage, statusColor } from "./mod-bisect-views";

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
  const [userSelectedGame, setUserSelectedGame] = useState<string>("");
  const [starting, setStarting] = useState(false);
  const startingRef = useRef(false);
  const excludeListRef = useRef<{ flush: () => Promise<string[] | null> }>(null);
  const { data: preserveD3dx = true } = useSetting("general.bisectPreserveD3dx");

  const { data: games = [] } = useGames();
  const defaultGame = games.find((g) => !isNteImporter(g.importer))?.game ?? "";
  const selectedGame = userSelectedGame || defaultGame;

  const { data: snapshot } = useQuery<BisectSnapshot | null>({
    queryKey: ["tools:bisectState"],
    queryFn: async () => (await Tools.BisectGetState()) as BisectSnapshot | null,
    refetchInterval: (query) => {
      const value = query.state.data;
      if (!value) return false;
      if (value.status === "scanning" || value.status === "reverting") return 500;
      return false;
    },
  });

  useEffect(() => {
    const off = Events.On("tools:bisectState", (event) => {
      queryClient.setQueryData(["tools:bisectState"], event.data);
    });
    return off;
  }, [queryClient]);

  const startMutation = useMutation({
    mutationFn: ({ game, excludePaths }: { game: string; excludePaths: string[] }) =>
      Tools.BisectStart(game, excludePaths),
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
    mutationFn: (fixed: boolean) => Tools.BisectRespond(fixed),
    onError: (err) => {
      toast.error(err.message);
      void queryClient.invalidateQueries({ queryKey: ["tools:bisectState"] });
    },
  });

  const undoMutation = useMutation({
    mutationFn: () => Tools.BisectUndoLastRound(),
    onError: (err) => toast.error(err.message),
  });

  const cancelMutation = useMutation({
    mutationFn: () => Tools.BisectCancel(),
    onError: (err) => toast.error(err.message),
  });

  const finalizeMutation = useMutation({
    mutationFn: (keepDisabled: string[]) => Tools.BisectFinalize(keepDisabled),
    onError: (err) => toast.error(err.message),
  });

  const recoverMutation = useMutation({
    mutationFn: (game: string) => Tools.BisectRecover(game),
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
              onChange={setUserSelectedGame}
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
            key={selectedGame}
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

      <BisectStatusCards
        snapshot={snapshot}
        busy={isBusy}
        onRespond={(fixed) => respondMutation.mutate(fixed)}
        onUndo={() => undoMutation.mutate()}
        onFinalize={(keepDisabled) => finalizeMutation.mutate(keepDisabled)}
      />
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
        const relative = await Tools.BisectValidateExcludePath(game, trimmed);
        const current = rowsRef.current.find((entry) => entry.id === id);
        if (!current || current.value.trim() !== trimmed) {
          committingRef.current.delete(id);
          if (!current) return true;
          if (current.value.trim()) return commitRow(id);
          lastFailedRef.current.delete(id);
          const next = rowsRef.current.filter((entry) => entry.id !== id);
          rowsRef.current = next;
          setRows(next);
          return true;
        }
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
        if (!(await commitRow(id))) return false;
        const next = rowsRef.current.find((entry) => entry.id === id)?.committed;
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
