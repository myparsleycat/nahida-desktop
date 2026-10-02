import { Mod } from "@bindings/mod";
import { Setting } from "@bindings/setting";
import { Tools } from "@bindings/tools";
import { XXMI } from "@bindings/xxmi";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import type { NamespaceIsolationState } from "@shared/types";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Button } from "../ui/button";
import { ScrollArea } from "../ui/scroll-area";

const namespaceStateKey = ["mod:getNamespaceIsolationState"];

function newerNamespaceState(
  previous: NamespaceIsolationState | undefined,
  incoming: NamespaceIsolationState,
) {
  return previous && previous.revision >= incoming.revision ? previous : incoming;
}

export default function TogglePersistence() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });

  const { data: enabled, isPending: isQueryPending } = useQuery({
    queryKey: ["setting:xxmi:getPersistToggles"],
    queryFn: () => Setting.GetPersistToggles(),
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

  if (!xxmiData?.xxmiPath) {
    return (
      <div className="flex w-full flex-col items-center justify-center p-2 text-center">
        <h3 className="text-lg font-semibold text-muted-foreground">
          {t("page.setting.xxmi.persistToggles")}
        </h3>
        <p className="text-sm text-muted-foreground italic">
          {t("page.setting.xxmi.persistNotFoundXXMI")}
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col space-y-4 p-4">
      <SectionContent>
        <SectionRow
          title={t("page.setting.xxmi.persistToggles")}
          description={t("page.setting.xxmi.persistTogglesDescription")}
        >
          <Switch
            checked={!!enabled}
            onCheckedChange={(c) => mutate(c)}
            disabled={isQueryPending || isMutatePending}
          />
        </SectionRow>
      </SectionContent>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.xxmi.namespaceIsolation.title")}</SectionTitle>
        </SectionHeader>
        <SectionContent layout="flow">
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.namespaceIsolation.description")}
          </p>
          {!enabled && !isQueryPending && (
            <p className="text-xs text-muted-foreground">
              {t("page.setting.xxmi.namespaceIsolation.disabled")}
            </p>
          )}
          <div role="status" className="text-xs text-muted-foreground">
            {namespaceQuery.data?.checking || namespaceQuery.isPending
              ? t("page.setting.xxmi.namespaceIsolation.checking")
              : namespaceQuery.isError
                ? t("page.setting.xxmi.namespaceIsolation.queryFailed")
                : (namespaceQuery.data?.conflicts ?? []).length === 0
                  ? t("page.setting.xxmi.namespaceIsolation.empty")
                  : null}
          </div>
          {namespaceQuery.isError && (
            <p className="text-xs break-all whitespace-pre-wrap text-destructive">
              {namespaceQuery.error.message}
            </p>
          )}
          {importerKeys.map((importerKey) => (
            <div key={importerKey} className="min-w-0 space-y-2">
              <SectionRow title={importerKey}>
                <Button
                  size="xs"
                  variant="outline"
                  aria-label={t("page.setting.xxmi.namespaceIsolation.rescanImporter", {
                    importer: importerKey,
                  })}
                  disabled={isMutatePending || rescan.isPending || namespaceQuery.data?.checking}
                  onClick={() => rescan.mutate(importerKey)}
                >
                  {t("page.setting.xxmi.namespaceIsolation.rescan")}
                </Button>
              </SectionRow>
              {(conflictsByImporter[importerKey] ?? []).map((conflict) => (
                <div key={conflict.id} className="space-y-1 rounded-md bg-muted/50 p-2 text-xs">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-mono break-all">{conflict.namespace}</span>
                    <span className="font-medium">
                      {t(`page.setting.xxmi.namespaceIsolation.status.${conflict.status}`, {
                        defaultValue: conflict.status,
                      })}
                    </span>
                  </div>
                  {conflict.reason && (
                    <p className="break-all whitespace-pre-wrap">
                      {t("page.setting.xxmi.namespaceIsolation.reason")}: {conflict.reason}
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
                    <div key={label}>
                      <p className="text-muted-foreground">
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
              ))}
            </div>
          ))}
        </SectionContent>
      </Section>

      <Section className="h-80">
        <SectionHeader>
          <SectionTitle>Logs</SectionTitle>
        </SectionHeader>
        <ScrollArea className="min-h-0 flex-1 overflow-auto rounded-md bg-muted/50 p-2">
          {logs.length === 0 ? (
            <div className="text-sm text-muted-foreground italic">No logs yet.</div>
          ) : (
            <div className="flex flex-col gap-1">
              {[...logs].reverse().map((log, index) => (
                <div key={`${log}-${index}`} className="font-mono text-xs break-all">
                  {log}
                </div>
              ))}
            </div>
          )}
        </ScrollArea>
      </Section>
    </div>
  );
}
