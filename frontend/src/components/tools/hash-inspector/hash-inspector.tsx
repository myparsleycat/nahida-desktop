import { Tools } from "@bindings/tools";
import { Status, type CaptureSummary, type State } from "@bindings/tools/hash_inspector";
import { Button } from "@renderer/components/ui/button";
import { Kbd, KbdGroup } from "@renderer/components/ui/kbd";
import {
  Section,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useEnabledImporters } from "@renderer/hooks/use-mod-data";
import { Logger } from "@renderer/lib/logger";
import { cn } from "@renderer/lib/utils";
import { formatKeyLabel } from "@shared/key-formatter";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { Loader2Icon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { CaptureDetail } from "./capture-detail";

const PICK_ERRORS = [
  ["HASH_INSPECTOR_NOT_CONNECTED:", "page.tools.hash_inspector.error.not_connected"],
  ["HASH_INSPECTOR_UNSUPPORTED:", "page.tools.hash_inspector.error.unsupported"],
  ["HASH_INSPECTOR_IN_PROGRESS:", "page.tools.hash_inspector.error.in_progress"],
] as const;

const STATUS_HINTS: Partial<Record<Status, string>> = {
  [Status.StatusNotRunning]: "page.tools.hash_inspector.status_hint.not-running",
  [Status.StatusConnecting]: "page.tools.hash_inspector.status_hint.connecting",
  [Status.StatusBusy]: "page.tools.hash_inspector.status_hint.busy",
};

const STATUS_DOTS: Partial<Record<Status, string>> = {
  [Status.StatusConnected]: "bg-emerald-500",
  [Status.StatusConnecting]: "bg-amber-500",
  [Status.StatusBusy]: "bg-amber-500",
  [Status.StatusUnsupported]: "bg-destructive",
  [Status.StatusIncompatible]: "bg-destructive",
};

const STATE_KEY = "tools:hashInspector:state";
const stateKey = (importer: string) => [STATE_KEY, importer];
const captureKey = (importer: string, id: string) => ["tools:hashInspector:capture", importer, id];

export default function HashInspector() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data: importers = [] } = useEnabledImporters();
  const [userImporter, setUserImporter] = useState("");
  const [userCapture, setUserCapture] = useState<{ importer: string; id: string } | null>(null);
  // A capture that arrived from the DLL is announced once, when its details have loaded.
  const arrivedCapture = useRef("");

  const importer = userImporter || importers?.[0]?.key || "";

  const { data: state } = useQuery({
    queryKey: stateKey(importer),
    queryFn: () => Tools.HashInspectorGetState(importer),
    enabled: !!importer,
    // The backend drops a pick the DLL never answers after a timeout, without an event.
    refetchInterval: (query) => (query.state.data?.picking ? 1000 : false),
  });
  const captures = state?.captures ?? [];
  const captureId =
    (userCapture?.importer === importer ? userCapture.id : undefined) ?? captures[0]?.id ?? "";

  const { data: capture } = useQuery({
    queryKey: captureKey(importer, captureId),
    queryFn: () => Tools.HashInspectorGetCapture(importer, captureId),
    enabled: !!importer && !!captureId,
  });

  useEffect(() => {
    const offState = Events.On("tools:hash-inspector-state", (event) => {
      const [changed] = eventArgs(event.data);
      void queryClient.invalidateQueries({
        queryKey: typeof changed === "string" && changed ? stateKey(changed) : [STATE_KEY],
      });
    });
    const offCapture = Events.On("tools:hash-inspector-capture", (event) => {
      const [changed, id] = eventArgs(event.data);
      if (typeof changed === "string" && typeof id === "string") {
        arrivedCapture.current = id;
        setUserCapture({ importer: changed, id });
      }
    });
    return () => {
      offState();
      offCapture();
    };
  }, [queryClient]);

  useEffect(() => {
    if (!capture || capture.id !== arrivedCapture.current) return;
    arrivedCapture.current = "";
    if (capture.failureCode) {
      toast.error(
        t(`page.tools.hash_inspector.failure.${capture.failureCode}`, {
          defaultValue: t("page.tools.hash_inspector.failure.title"),
        }),
      );
    } else if (capture.trigger === "key") {
      toast.success(t("page.tools.hash_inspector.captured_by_key"));
    }
  }, [capture, t]);

  const pickMutation = useMutation({
    mutationFn: () => Tools.HashInspectorPick(importer),
    onError: (err) => {
      const key = PICK_ERRORS.find(([code]) => err.message.startsWith(code))?.[1];
      if (!key) Logger.error(err, "HashInspector:pick");
      toast.error(key ? t(key) : err.message);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: stateKey(importer) }),
  });

  const deleteMutation = useMutation({
    mutationFn: () => Tools.HashInspectorDeleteCaptures(importer),
    onSuccess: () => setUserCapture(null),
    onError: (err) => toast.error(err.message),
    onSettled: () => queryClient.invalidateQueries({ queryKey: stateKey(importer) }),
  });

  const keyLabels = (state?.inspectKey ?? "")
    .split(" ")
    .map((key) => formatKeyLabel(key))
    .filter((key): key is string => key !== null);
  const connected = state?.status === Status.StatusConnected;
  const picking = !!state?.picking || pickMutation.isPending;
  const hintKey = state ? STATUS_HINTS[state.status] : undefined;
  const hint = hintKey ? t(hintKey) : state ? statusText(t, state) : "";

  return (
    <div className="flex h-full min-h-0 flex-col gap-6 overflow-y-auto p-4">
      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.tools.hash_inspector.title")}</SectionTitle>
          <SectionDescription>{t("page.tools.hash_inspector.pick_desc")}</SectionDescription>
        </SectionHeader>
        <SectionContent layout="flow">
          {importers?.length ? (
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={importer}
                onValueChange={(value) => typeof value === "string" && setUserImporter(value)}
              >
                <SelectTrigger
                  className="w-32"
                  aria-label={t("page.tools.hash_inspector.importer")}
                >
                  <SelectValue placeholder={t("page.tools.hash_inspector.select_importer")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {importers.map((item) => (
                      <SelectItem key={item.key} value={item.key}>
                        {item.key}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {state ? (
                <span className="flex min-w-0 items-center gap-1.5 text-xs">
                  <span
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      STATUS_DOTS[state.status] ?? "bg-muted-foreground",
                    )}
                  />
                  <span
                    className={cn(
                      connected ? "text-foreground" : "text-muted-foreground",
                      state.status === Status.StatusIncompatible && "text-destructive",
                    )}
                  >
                    {statusText(t, state)}
                  </span>
                </span>
              ) : null}
              <div className="ml-auto flex flex-wrap items-center gap-2">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button
                        onClick={() => pickMutation.mutate()}
                        disabled={!connected || picking}
                      />
                    }
                  >
                    {picking ? <Loader2Icon className="size-3.5 animate-spin" /> : null}
                    {picking
                      ? t("page.tools.hash_inspector.picking")
                      : t("page.tools.hash_inspector.pick")}
                  </TooltipTrigger>
                  <TooltipContent>
                    {connected || !state ? t("page.tools.hash_inspector.pick_desc") : hint}
                  </TooltipContent>
                </Tooltip>
                {state && state.status !== Status.StatusUnsupported ? (
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <span className="flex items-center gap-1.5 text-xs text-muted-foreground" />
                      }
                    >
                      {keyLabels.length ? (
                        <>
                          {t("page.tools.hash_inspector.or_in_game")}
                          <KbdGroup>
                            {keyLabels.map((label) => (
                              <Kbd key={label}>{label}</Kbd>
                            ))}
                          </KbdGroup>
                        </>
                      ) : (
                        `${t("page.tools.hash_inspector.key")}: ${t("page.tools.hash_inspector.key_none")}`
                      )}
                    </TooltipTrigger>
                    <TooltipContent>{t("page.tools.hash_inspector.key_desc")}</TooltipContent>
                  </Tooltip>
                ) : null}
              </div>
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t("page.tools.hash_inspector.no_importers")}
            </p>
          )}

          {state && !connected && hintKey ? (
            <p className="text-xs text-muted-foreground">{hint}</p>
          ) : null}
        </SectionContent>
      </Section>

      {importer ? (
        <Section>
          <SectionHeader className="flex flex-row items-center justify-between gap-2">
            <SectionTitle>{t("page.tools.hash_inspector.captures")}</SectionTitle>
            <Button
              variant="outline"
              size="sm"
              onClick={() => deleteMutation.mutate()}
              disabled={!captures.length || deleteMutation.isPending}
            >
              {t("page.tools.hash_inspector.delete_captures")}
            </Button>
          </SectionHeader>
          <SectionContent layout="flow">
            {captures.length ? (
              <div className="flex flex-wrap gap-1.5">
                {captures.map((item) => (
                  <CaptureChip
                    key={item.id}
                    item={item}
                    active={item.id === captureId}
                    onSelect={() => setUserCapture({ importer, id: item.id })}
                  />
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t("page.tools.hash_inspector.no_captures")}
              </p>
            )}
          </SectionContent>
        </Section>
      ) : null}

      {capture && capture.id === captureId ? (
        <CaptureDetail key={capture.id} capture={capture} />
      ) : null}
    </div>
  );
}

function CaptureChip({
  item,
  active,
  onSelect,
}: {
  item: CaptureSummary;
  active: boolean;
  onSelect: () => void;
}) {
  const { t } = useTranslation();
  const time = new Date(item.capturedAt).toLocaleTimeString();
  const detail = item.failureCode
    ? t("page.tools.hash_inspector.failure.title")
    : `${item.visible || "-"} · ${t("page.tools.hash_inspector.layers_count", { count: item.layers })}`;

  return (
    <Button variant={active ? "secondary" : "outline"} size="sm" onClick={onSelect}>
      <span className="font-mono">{time}</span>
      <span
        className={cn("text-xs", item.failureCode ? "text-destructive" : "text-muted-foreground")}
      >
        {t(`page.tools.hash_inspector.trigger.${item.trigger}`)} · {detail}
      </span>
    </Button>
  );
}

function statusText(t: (key: string, options?: Record<string, unknown>) => string, state: State) {
  switch (state.status) {
    case Status.StatusConnected:
      return t("page.tools.hash_inspector.status.connected", { version: state.dllVersion || "-" });
    case Status.StatusUnsupported:
      return t(`page.tools.hash_inspector.status.unsupported_${state.reason || "provider"}`);
    case Status.$zero:
      return "";
    default:
      return t(`page.tools.hash_inspector.status.${state.status}`);
  }
}

// Wails passes a single emitted value as is and several values as an array.
function eventArgs(data: unknown): unknown[] {
  return Array.isArray(data) ? data : [data];
}
