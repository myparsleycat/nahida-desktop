import { Mod } from "@bindings/mod";
import { Setting } from "@bindings/setting";
import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import { Badge } from "@renderer/components/ui/badge";
import {
  Section,
  SectionAction,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { cn } from "@renderer/lib/utils";
import type { NamespaceIsolationState } from "@shared/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import {
  AlertTriangleIcon,
  ArrowRightIcon,
  ChevronRightIcon,
  CircleCheckIcon,
  CircleXIcon,
  FolderSearchIcon,
  Loader2Icon,
  RefreshCwIcon,
} from "lucide-react";
import { useEffect, useId } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Button } from "../ui/button";

const namespaceStateKey = ["mod:getNamespaceIsolationState"];

// Backend entries look like "[Jan 2, 2006, 3:04:05 PM] [INFO] message".
const logPattern = /^\[(.+?)\] \[(INFO|ERROR)\] ([\s\S]*)$/;

const conflictStatusClass: Record<string, string> = {
  waiting_for_game_exit: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  needs_review: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  failed: "bg-destructive/10 text-destructive dark:bg-destructive/20",
  recovery_required: "bg-destructive/10 text-destructive dark:bg-destructive/20",
};

function newerNamespaceState(
  previous: NamespaceIsolationState | undefined,
  incoming: NamespaceIsolationState,
) {
  return previous && previous.revision >= incoming.revision ? previous : incoming;
}

export default function TogglePersistence() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const titleId = useId();
  const isolationTitleId = useId();

  const { data: xxmiData, isPending: isXXMIPending } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  const { data: enabled, isPending: isQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getPersistToggles"],
    queryFn: () => Setting.GetPersistToggles(),
  });

  const { data: isolationEnabled, isPending: isIsolationQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getNamespaceIsolation"],
    queryFn: () => Setting.GetNamespaceIsolation(),
  });

  const { data: logs = [] } = useQuery<string[]>({
    queryKey: ["setting:xxmi:getPersistLogs"],
    queryFn: async () => (await Tools.GetPersistLogs()) ?? [],
  });

  const namespaceQuery = useQuery<NamespaceIsolationState>({
    queryKey: namespaceStateKey,
    queryFn: () => Mod.GetNamespaceIsolationState(),
    // Query responses can arrive after a newer event or rescan response.
    structuralSharing: (previous, incoming) =>
      newerNamespaceState(
        previous as NamespaceIsolationState | undefined,
        incoming as NamespaceIsolationState,
      ),
  });

  const rescan = useMutation({
    mutationFn: (importerKey: string) => Mod.RescanNamespaceIsolation(importerKey),
    onSuccess: (state) => {
      queryClient.setQueryData<NamespaceIsolationState>(namespaceStateKey, (previous) =>
        newerNamespaceState(previous, state),
      );
    },
    onError: (err) => {
      toast.error(t("page.setting.xxmi.namespaceIsolation.rescanFailed"), {
        description: err.message,
      });
    },
  });

  useEffect(() => {
    const off = Events.On("setting:xxmi:persistLogs", (event) => {
      queryClient.setQueryData(["setting:xxmi:getPersistLogs"], event.data);
    });

    const offNamespace = Events.On("mod:namespace-isolation-state", (event) => {
      queryClient.setQueryData<NamespaceIsolationState>(namespaceStateKey, (previous) =>
        newerNamespaceState(previous, event.data),
      );
    });

    return () => {
      off();
      offNamespace();
    };
  }, [queryClient]);

  const { mutate, isPending: isMutatePending } = useMutation({
    mutationFn: (newEnabled: boolean) => Setting.SetPersistToggles(newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getPersistToggles"], newEnabled);
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  const isolation = useMutation({
    mutationFn: (newEnabled: boolean) => Setting.SetNamespaceIsolation(newEnabled),
    onSuccess: (_, newEnabled) => {
      queryClient.setQueryData(["setting:xxmi:getNamespaceIsolation"], newEnabled);
    },
    onError: (err) => {
      toast.error(err.message);
    },
  });

  const conflictsByImporter = groupBy(
    namespaceQuery.data?.conflicts ?? [],
    (conflict) => conflict.importerKey,
  );
  const importerKeys = [
    ...new Set([
      ...(xxmiData?.enabledImporters ?? []).map((importer) => importer.key),
      ...Object.keys(conflictsByImporter),
    ]),
  ].filter((key) => key.toUpperCase() !== "NTE");
  const visibleConflicts = importerKeys.flatMap((key) => conflictsByImporter[key] ?? []);
  const isChecking = namespaceQuery.data?.checking || namespaceQuery.isPending;

  if (isXXMIPending) return null;

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center">
        <FolderSearchIcon className="size-8 text-muted-foreground" />
        <div className="space-y-1">
          <h3 className="text-sm font-medium text-foreground">
            {t("page.setting.xxmi.persistToggles")}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistNotFoundXXMI")}
          </p>
        </div>
        <Button render={<Link to="/xxmi" />} variant="outline" size="sm">
          {t("page.setting.xxmi.xxmiPath")}
          <ArrowRightIcon />
        </Button>
      </div>
    );
  }

  return (
    <div className="h-full space-y-6 overflow-y-auto p-4">
      <div className="flex items-start justify-between gap-6">
        <div className="min-w-0 space-y-1">
          <h2 id={titleId} className="text-lg font-semibold text-foreground">
            {t("page.setting.xxmi.persistToggles")}
          </h2>
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistTogglesDescription")}
          </p>
        </div>
        <Switch
          aria-labelledby={titleId}
          className="mt-1"
          checked={!!enabled}
          onCheckedChange={(c) => mutate(c)}
          disabled={isQueryPending || isMutatePending}
        />
      </div>

      <Section>
        <SectionHeader>
          <SectionTitle id={isolationTitleId}>
            {t("page.setting.xxmi.namespaceIsolation.title")}
          </SectionTitle>
          <SectionDescription>
            {t("page.setting.xxmi.namespaceIsolation.description")}
          </SectionDescription>
          <SectionAction>
            <Switch
              aria-labelledby={isolationTitleId}
              checked={!!isolationEnabled}
              onCheckedChange={(c) => isolation.mutate(c)}
              disabled={isIsolationQueryPending || isolation.isPending || isMutatePending}
            />
          </SectionAction>
        </SectionHeader>
        <SectionContent layout="flow">
          {/* Isolation only runs alongside toggle persistence, so its switch alone does nothing. */}
          {isolationEnabled && !enabled && !isQueryPending && (
            <div className="flex items-start gap-2 rounded-md border border-amber-500/20 bg-amber-500/10 p-2 text-xs text-amber-600 dark:text-amber-400">
              <AlertTriangleIcon className="mt-px size-3.5 shrink-0" />
              <p>{t("page.setting.xxmi.namespaceIsolation.disabled")}</p>
            </div>
          )}

          <div role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
            {isChecking ? (
              <>
                <Loader2Icon className="size-3.5 shrink-0 animate-spin" />
                <span>{t("page.setting.xxmi.namespaceIsolation.checking")}</span>
              </>
            ) : namespaceQuery.isError ? (
              <>
                <CircleXIcon className="size-3.5 shrink-0 text-destructive" />
                <span className="text-destructive">
                  {t("page.setting.xxmi.namespaceIsolation.queryFailed")}
                </span>
              </>
            ) : (namespaceQuery.data?.conflicts ?? []).length === 0 ? (
              <>
                <CircleCheckIcon className="size-3.5 shrink-0 text-green-600 dark:text-green-400" />
                <span>{t("page.setting.xxmi.namespaceIsolation.empty")}</span>
              </>
            ) : null}
          </div>
          {namespaceQuery.isError && (
            <p className="rounded-md bg-destructive/10 p-2 font-mono text-xs break-all whitespace-pre-wrap text-destructive">
              {namespaceQuery.error.message}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-1.5">
            <span className="mr-1 text-xs text-muted-foreground">
              {t("page.setting.xxmi.namespaceIsolation.rescan")}
            </span>
            {importerKeys.map((importerKey) => {
              const label = t("page.setting.xxmi.namespaceIsolation.rescanImporter", {
                importer: importerKey,
              });

              return (
                <Button
                  key={importerKey}
                  size="xs"
                  variant="outline"
                  className="font-mono"
                  aria-label={label}
                  title={label}
                  disabled={
                    isMutatePending ||
                    isolation.isPending ||
                    rescan.isPending ||
                    namespaceQuery.data?.checking
                  }
                  onClick={() => rescan.mutate(importerKey)}
                >
                  <RefreshCwIcon
                    className={cn(
                      rescan.isPending && rescan.variables === importerKey && "animate-spin",
                    )}
                  />
                  {importerKey}
                </Button>
              );
            })}
          </div>

          {visibleConflicts.length > 0 && (
            // One list for every importer, scrolling on its own, so neither the importer count nor the conflict
            // count can stretch the page.
            <div className="max-h-64 divide-y divide-border/60 overflow-y-auto rounded-md border border-border/60 bg-muted/30 text-xs">
              {visibleConflicts.map((conflict) => (
                // Collapsed by default: paths make each conflict tall, and a native details element keeps the
                // content in the DOM without per-row state.
                <details key={conflict.id} className="group">
                  <summary className="flex cursor-pointer list-none items-center gap-2 px-2 py-1 hover:bg-muted/50 [&::-webkit-details-marker]:hidden">
                    <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
                    <span className="w-10 shrink-0 font-mono text-muted-foreground">
                      {conflict.importerKey}
                    </span>
                    {/* Issues such as invalid metadata carry no namespace; the reason names them instead. */}
                    <span
                      className="min-w-0 flex-1 truncate font-mono font-medium group-open:break-all group-open:whitespace-normal"
                      title={conflict.namespace || conflict.reason}
                    >
                      {conflict.namespace || conflict.reason}
                    </span>
                    <Badge variant="secondary" className={conflictStatusClass[conflict.status]}>
                      {t(`page.setting.xxmi.namespaceIsolation.status.${conflict.status}`, {
                        defaultValue: conflict.status,
                      })}
                    </Badge>
                  </summary>
                  <div className="space-y-2 border-t border-border/60 p-3">
                    {conflict.namespace && conflict.reason && (
                      <p className="break-all whitespace-pre-wrap text-muted-foreground">
                        {t("page.setting.xxmi.namespaceIsolation.reason")}:{" "}
                        <span className="font-mono text-foreground">{conflict.reason}</span>
                      </p>
                    )}
                    {conflict.detail && (
                      <p className="break-all whitespace-pre-wrap">{conflict.detail}</p>
                    )}
                    {(
                      [
                        ["modPaths", conflict.modPaths],
                        ["iniPaths", conflict.iniPaths],
                      ] as const
                    ).map(([label, paths]) => (
                      <div key={label} className="space-y-1">
                        <p className="text-[10px] font-medium tracking-widest text-muted-foreground uppercase">
                          {t(`page.setting.xxmi.namespaceIsolation.${label}`)}
                        </p>
                        <ul className="space-y-0.5 font-mono break-all">
                          {(paths ?? []).map((path) => (
                            <li key={path}>{path}</li>
                          ))}
                        </ul>
                      </div>
                    ))}
                  </div>
                </details>
              ))}
            </div>
          )}
        </SectionContent>
      </Section>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.xxmi.persistLogs")}</SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          {logs.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("page.setting.xxmi.persistLogsEmpty")}
            </p>
          ) : (
            <ol className="space-y-1.5 rounded-md bg-muted/50 p-2 font-mono text-xs">
              {[...logs].reverse().map((log, index) => {
                const match = logPattern.exec(log);
                if (!match) {
                  return (
                    <li key={`${log}-${index}`} className="break-all">
                      {log}
                    </li>
                  );
                }

                const isError = match[2] === "ERROR";
                return (
                  <li key={`${log}-${index}`} className="flex flex-col gap-x-2 sm:flex-row">
                    <span className="shrink-0 text-muted-foreground">{match[1]}</span>
                    <span
                      className={cn(
                        "w-10 shrink-0 font-medium",
                        isError ? "text-destructive" : "text-muted-foreground",
                      )}
                    >
                      {match[2]}
                    </span>
                    <span
                      className={cn(
                        "min-w-0 break-all whitespace-pre-wrap",
                        isError && "text-destructive",
                      )}
                    >
                      {match[3]}
                    </span>
                  </li>
                );
              })}
            </ol>
          )}
        </SectionContent>
      </Section>
    </div>
  );
}
