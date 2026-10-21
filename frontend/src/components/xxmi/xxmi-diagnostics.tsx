import { Shell } from "@bindings/platform";
import { XXMI } from "@bindings/xxmi";
import {
  DiagnosticsLevel,
  DiagnosticsState,
  type DiagnosticsBundle,
  type DiagnosticsMod,
  type DiagnosticsSession,
} from "@bindings/xxmi/models";
import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
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
import { Progress } from "@renderer/components/ui/progress";
import {
  Section,
  SectionContent,
  SectionDescription,
  SectionHeader,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Textarea } from "@renderer/components/ui/textarea";
import { DiagnosticsModPicker } from "@renderer/components/xxmi/xxmi-diagnostics-mods";
import { ToggleRow } from "@renderer/components/xxmi/xxmi-fields";
import { useAuth } from "@renderer/hooks/use-auth";
import { useFeedbackSubmit } from "@renderer/hooks/use-feedback-submit";
import { cn } from "@renderer/lib/utils";
import { formatSize, toErrorMessage } from "@shared/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { BugIcon, FolderOpenIcon, Loader2Icon, PlayIcon, TriangleAlertIcon } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const LEVELS = [DiagnosticsLevel.DiagnosticsInfo, DiagnosticsLevel.DiagnosticsDebug] as const;
const errorCodePattern = /(XXMI_[A-Z_]+)(?::([^\n]*))?/;

export function diagnosticsQueryKey(importer: string) {
  return ["xxmi:diagnostics", importer] as const;
}

export function diagnosticsSupportQueryKey(importer: string) {
  return ["xxmi:diagnostics-support", importer] as const;
}

/** Reads the importer's logging session and follows the backend's changes to it. */
export function useDiagnosticsSession(importer: string) {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: diagnosticsQueryKey(importer),
    queryFn: () => XXMI.GetDiagnostics(importer),
  });

  useEffect(() => {
    return Events.On("xxmi:diagnostics-changed", (event) => {
      const changed = (event.data as { importer?: string } | undefined)?.importer;
      if (changed === importer) {
        void queryClient.invalidateQueries({ queryKey: diagnosticsQueryKey(importer) });
      }
    });
  }, [importer, queryClient]);

  return data ?? null;
}

function useDiagnosticsErrorMessage() {
  const { t } = useTranslation();
  return (error: unknown) => {
    const message = toErrorMessage(error);
    const match = errorCodePattern.exec(message);
    return match
      ? t(`page.setting.xxmi.diagnostics.errors.${match[1]}`, {
          detail: match[2] ?? "",
          defaultValue: message,
        })
      : message;
  };
}

/** Points at a running or collected logging session from the importer's header. */
export function XXMIDiagnosticsNotice({
  importer,
  onOpen,
}: {
  importer: string;
  onOpen: () => void;
}) {
  const { t } = useTranslation();
  const session = useDiagnosticsSession(importer);
  if (!session) return null;

  return (
    <Alert>
      <BugIcon />
      <AlertTitle>
        {t(
          session.state === DiagnosticsState.DiagnosticsArmed
            ? "page.setting.xxmi.diagnostics.noticeArmed"
            : "page.setting.xxmi.diagnostics.noticeCollected",
        )}
      </AlertTitle>
      <AlertDescription>
        <Button variant="link" size="sm" className="h-auto p-0" onClick={onOpen}>
          {t("page.setting.xxmi.diagnostics.noticeOpen")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

export function XXMIDiagnostics({
  importer,
  running,
  launchDisabled,
  onLaunch,
}: {
  importer: string;
  running: boolean;
  launchDisabled: boolean;
  onLaunch: () => Promise<unknown>;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const errorMessage = useDiagnosticsErrorMessage();
  const { data: support } = useQuery({
    queryKey: diagnosticsSupportQueryKey(importer),
    queryFn: () => XXMI.DiagnosticsSupport(importer),
  });
  const session = useDiagnosticsSession(importer);
  const [starting, setStarting] = useState(false);
  const [reporting, setReporting] = useState(false);

  const refresh = () => queryClient.invalidateQueries({ queryKey: diagnosticsQueryKey(importer) });
  const end = async () => {
    try {
      await XXMI.EndDiagnostics(importer);
      toast.success(t("page.setting.xxmi.diagnostics.ended"));
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      void refresh();
    }
  };
  const launch = async () => {
    try {
      await onLaunch();
    } catch (error) {
      toast.error(toErrorMessage(error));
    }
  };

  const supported = support?.supported === true;
  const armed = session?.state === DiagnosticsState.DiagnosticsArmed;
  const collected = session?.state === DiagnosticsState.DiagnosticsCollected;

  return (
    <Section>
      <SectionHeader>
        <SectionTitle>{t("page.setting.xxmi.diagnostics.title")}</SectionTitle>
        <SectionDescription>{t("page.setting.xxmi.diagnostics.description")}</SectionDescription>
      </SectionHeader>
      <SectionContent layout="flow" className="gap-3">
        {!session && support && !supported ? (
          <Alert>
            <TriangleAlertIcon />
            <AlertDescription>
              {t(`page.setting.xxmi.diagnostics.unsupported.${support?.reason || "provider"}`, {
                defaultValue: t("page.setting.xxmi.diagnostics.unsupported.provider"),
              })}
            </AlertDescription>
          </Alert>
        ) : null}

        {armed ? (
          <Alert>
            <BugIcon />
            <AlertTitle>
              {t("page.setting.xxmi.diagnostics.armedTitle", {
                level: t(`page.setting.xxmi.diagnostics.level.${session.level}.label`),
              })}
            </AlertTitle>
            <AlertDescription className="space-y-1">
              <p>
                {/* A launch is recorded before the game starts, so a game running without one was
                    started before the session and read d3dx.ini without its options. */}
                {t(
                  running
                    ? session.launchedAt
                      ? "page.setting.xxmi.diagnostics.armedRunning"
                      : "page.setting.xxmi.diagnostics.armedRelaunch"
                    : session.launchedAt
                      ? "page.setting.xxmi.diagnostics.armedLaunched"
                      : "page.setting.xxmi.diagnostics.armedReady",
                )}
              </p>
              <p>{t("page.setting.xxmi.diagnostics.hangHint")}</p>
              <p>{t("page.setting.xxmi.diagnostics.lastLaunchHint")}</p>
            </AlertDescription>
          </Alert>
        ) : null}

        {collected ? (
          <Alert>
            <BugIcon />
            <AlertTitle>{t("page.setting.xxmi.diagnostics.collectedTitle")}</AlertTitle>
            <AlertDescription>
              {t("page.setting.xxmi.diagnostics.collectedDescription")}
            </AlertDescription>
          </Alert>
        ) : null}

        <div className="flex flex-wrap gap-2">
          {!session && supported ? (
            <Button size="sm" onClick={() => setStarting(true)}>
              <BugIcon />
              {t("page.setting.xxmi.diagnostics.start")}
            </Button>
          ) : null}
          {armed ? (
            <>
              <Button
                size="sm"
                variant="outline"
                disabled={running || launchDisabled}
                onClickPromise={launch}
              >
                <PlayIcon />
                {t("page.setting.xxmi.diagnostics.launch")}
              </Button>
              <Button size="sm" disabled={running} onClick={() => setReporting(true)}>
                {t("page.setting.xxmi.diagnostics.collect")}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setStarting(true)}>
                {t("page.setting.xxmi.diagnostics.changeOptions")}
              </Button>
            </>
          ) : null}
          {collected ? (
            <>
              <Button size="sm" onClick={() => setReporting(true)}>
                {t("page.setting.xxmi.diagnostics.report")}
              </Button>
              {supported ? (
                <Button size="sm" variant="outline" onClick={() => setStarting(true)}>
                  {t("page.setting.xxmi.diagnostics.restart")}
                </Button>
              ) : null}
            </>
          ) : null}
          {session ? (
            <Button size="sm" variant="outline" onClickPromise={end}>
              {t(
                armed
                  ? "page.setting.xxmi.diagnostics.end"
                  : "page.setting.xxmi.diagnostics.discard",
              )}
            </Button>
          ) : null}
        </div>
        {armed && running ? (
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.diagnostics.collectWhenClosed")}
          </p>
        ) : null}
      </SectionContent>

      {starting ? (
        <StartDialog
          importer={importer}
          current={armed ? session : null}
          canLaunch={!running && !launchDisabled}
          onClose={() => setStarting(false)}
          onStarted={async (launchNow) => {
            setStarting(false);
            await refresh();
            if (launchNow) await launch();
          }}
        />
      ) : null}
      {reporting && session ? (
        <ReportDialog
          importer={importer}
          sessionStartedAt={session.startedAt}
          onClose={() => {
            setReporting(false);
            void refresh();
          }}
        />
      ) : null}
    </Section>
  );
}

function StartDialog({
  importer,
  current,
  canLaunch,
  onClose,
  onStarted,
}: {
  importer: string;
  current: DiagnosticsSession | null;
  canLaunch: boolean;
  onClose: () => void;
  onStarted: (launchNow: boolean) => Promise<void>;
}) {
  const { t } = useTranslation();
  const errorMessage = useDiagnosticsErrorMessage();
  const [level, setLevel] = useState<DiagnosticsLevel>(
    current?.level || DiagnosticsLevel.DiagnosticsInfo,
  );
  const [debugLocks, setDebugLocks] = useState(current?.debugLocks ?? false);
  const [busy, setBusy] = useState(false);

  const start = async () => {
    setBusy(true);
    try {
      await XXMI.StartDiagnostics(importer, { level, debugLocks });
      toast.success(t("page.setting.xxmi.diagnostics.started"));
      await onStarted(!current && canLaunch);
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="sm:max-w-lg" showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>{t("page.setting.xxmi.diagnostics.startTitle")}</DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.diagnostics.startDescription")}
          </DialogDescription>
        </DialogHeader>
        <div role="radiogroup" className="grid gap-2">
          {LEVELS.map((value) => (
            <button
              key={value}
              type="button"
              role="radio"
              aria-checked={level === value}
              disabled={busy}
              className={cn(
                "rounded-lg border px-3 py-2.5 text-left transition-colors hover:bg-muted/50",
                level === value && "border-primary bg-muted/50",
              )}
              onClick={() => setLevel(value)}
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                {t(`page.setting.xxmi.diagnostics.level.${value}.label`)}
                {value === DiagnosticsLevel.DiagnosticsInfo ? (
                  <Badge variant="secondary">
                    {t("page.setting.xxmi.diagnostics.recommended")}
                  </Badge>
                ) : null}
              </span>
              <span className="mt-0.5 block text-xs text-muted-foreground">
                {t(`page.setting.xxmi.diagnostics.level.${value}.description`)}
              </span>
            </button>
          ))}
        </div>
        <ToggleRow
          label={t("page.setting.xxmi.diagnostics.debugLocks")}
          description={t("page.setting.xxmi.diagnostics.debugLocksDescription")}
          checked={debugLocks}
          onCheckedChange={setDebugLocks}
        />
        <p className="text-xs text-muted-foreground">
          {t("page.setting.xxmi.diagnostics.alwaysOn")}
        </p>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t("g.cancel")}
          </Button>
          <Button disabled={busy} onClick={() => void start()}>
            {busy && <Loader2Icon className="animate-spin" />}
            {t(
              current
                ? "page.setting.xxmi.diagnostics.apply"
                : canLaunch
                  ? "page.setting.xxmi.diagnostics.startAndLaunch"
                  : "page.setting.xxmi.diagnostics.start",
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ReportDialog({
  importer,
  sessionStartedAt,
  onClose,
}: {
  importer: string;
  sessionStartedAt: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { session } = useAuth();
  const errorMessage = useDiagnosticsErrorMessage();
  const logCheckboxId = useId();
  const { rules, rulesFailed, retryRules, progress, sending, problem, submit, cancel } =
    useFeedbackSubmit();
  const [body, setBody] = useState("");
  const [includeLog, setIncludeLog] = useState(true);
  const [mods, setMods] = useState<ReadonlyMap<string, DiagnosticsMod>>(new Map());

  // Collecting again returns the bundle already written, so a refetch does no harm. The bundle belongs to one
  // session, whose end deletes it, so it is neither shared with a later session nor kept after the dialog.
  const bundleQuery = useQuery({
    queryKey: ["xxmi:diagnostics-bundle", importer, sessionStartedAt, rules?.maxMediaBytes],
    queryFn: () => XXMI.CollectDiagnostics(importer, rules?.maxMediaBytes ?? 0),
    enabled: !!rules,
    retry: false,
    refetchOnWindowFocus: false,
    gcTime: 0,
  });
  // Only a collection of this session that succeeded and is not being redone can be sent.
  const bundle: DiagnosticsBundle | undefined =
    bundleQuery.isSuccess && !bundleQuery.isFetching ? bundleQuery.data : undefined;

  const reportProblem = bundle ? problem({ body, mediaFiles: 1, mediaBytes: bundle.size }) : null;

  const send = async () => {
    if (!bundle) return;
    const result = await submit({
      category: "xxmi-libs",
      body,
      includeLog,
      files: [bundle.path],
      modDirs: session
        ? [...mods.values()].map((mod) => ({ path: mod.path, name: mod.relative }))
        : [],
    });
    if (!result) return;
    try {
      await XXMI.EndDiagnostics(importer);
    } catch (error) {
      toast.error(errorMessage(error));
    }
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !sending && onClose()}>
      <DialogContent
        className="flex max-h-[85vh] flex-col overflow-hidden sm:max-w-xl"
        showCloseButton={!sending}
      >
        <DialogHeader>
          <DialogTitle>{t("page.setting.xxmi.diagnostics.reportTitle")}</DialogTitle>
          <DialogDescription>
            {t("page.setting.xxmi.diagnostics.reportDescription")}
          </DialogDescription>
        </DialogHeader>

        <div className="-mx-4 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-0.5">
          {/* The bundle is sized to the rules, so without them it is never collected. */}
          {rulesFailed ? (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription className="flex flex-wrap items-center gap-x-2">
                {t("page.setting.feedback.error.rules_unavailable")}
                <Button variant="link" size="sm" className="h-auto p-0" onClick={retryRules}>
                  {t("page.setting.feedback.retry_rules")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : bundleQuery.isError ? (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription>{errorMessage(bundleQuery.error)}</AlertDescription>
            </Alert>
          ) : !bundle ? (
            <p className="flex items-center gap-2 text-muted-foreground">
              <Loader2Icon className="size-4 animate-spin" />
              {t("page.setting.xxmi.diagnostics.collecting")}
            </p>
          ) : (
            <div className="space-y-1 rounded-md bg-muted/50 px-3 py-2">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate font-medium" title={bundle.path}>
                  {bundle.path.slice(bundle.path.lastIndexOf("\\") + 1)}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                  {formatSize(bundle.size)}
                </span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={t("page.setting.xxmi.diagnostics.openFolder")}
                  onClick={() =>
                    void Shell.OpenPath(bundle.path.slice(0, bundle.path.lastIndexOf("\\"))).catch(
                      (error: unknown) => toast.error(toErrorMessage(error)),
                    )
                  }
                >
                  <FolderOpenIcon />
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.xxmi.diagnostics.bundleSummary", {
                  log: bundle.logBytes > 0 ? formatSize(bundle.logBytes) : "-",
                  dumps: bundle.dumps,
                  events: bundle.crashEvents,
                })}
              </p>
              {bundle.logTruncated ? (
                <p className="text-xs text-muted-foreground">
                  {t("page.setting.xxmi.diagnostics.logTruncated")}
                </p>
              ) : null}
            </div>
          )}

          <div className="space-y-2">
            <span className="text-sm font-medium">{t("page.setting.feedback.body.label")}</span>
            <Textarea
              value={body}
              disabled={sending}
              placeholder={t("page.setting.xxmi.diagnostics.bodyPlaceholder")}
              className="max-h-60 min-h-28"
              onChange={(event) => setBody(event.target.value)}
            />
          </div>

          <div className="space-y-2">
            <span className="text-sm font-medium">{t("page.setting.xxmi.diagnostics.mods")}</span>
            <p className="text-xs text-muted-foreground">
              {t(
                session
                  ? "page.setting.xxmi.diagnostics.modsDescription"
                  : "page.setting.feedback.attachments.mod_login_required",
              )}
            </p>
            <DiagnosticsModPicker
              importer={importer}
              selected={mods}
              disabled={!session || sending}
              onSelectedChange={setMods}
            />
          </div>

          <div className="flex items-start gap-2.5">
            <Checkbox
              id={logCheckboxId}
              className="mt-0.5"
              checked={includeLog}
              disabled={sending}
              onCheckedChange={(checked) => setIncludeLog(checked)}
            />
            <label htmlFor={logCheckboxId} className="space-y-0.5">
              <span className="text-sm font-medium">{t("page.setting.feedback.log.label")}</span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.feedback.log.description")}
              </p>
            </label>
          </div>

          {sending && <Progress value={progress} />}
          {reportProblem && <p className="text-xs text-destructive">{reportProblem}</p>}
        </div>

        <DialogFooter>
          {sending ? (
            <Button variant="outline" onClick={cancel}>
              {t("page.setting.feedback.cancel")}
            </Button>
          ) : (
            <Button variant="outline" onClick={onClose}>
              {t("g.close")}
            </Button>
          )}
          <Button
            disabled={sending || !rules || !bundle || body.trim() === "" || reportProblem !== null}
            onClick={() => void send()}
          >
            {sending && <Loader2Icon className="animate-spin" />}
            {t(sending ? "page.setting.feedback.sending" : "page.setting.feedback.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
