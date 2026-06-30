import { Button } from "@renderer/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@renderer/components/ui/tabs";
import {
  CircleCheckIcon,
  CircleXIcon,
  HammerIcon,
  Loader2Icon,
  RotateCcwIcon,
  ShieldCheckIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

interface XXMIImporter {
  key: string;
  importerFolder: string;
}

type FixerTask = "build-dll" | "diversify-dll" | "restore-dll";

export default function FourThousandOneFixer() {
  const { t } = useTranslation();

  const [provider, setProvider] = useState("SpectrumQT");
  const [versions, setVersions] = useState<string[] | null>(null);
  const [version, setVersion] = useState("");

  const [importers, setImporters] = useState<XXMIImporter[]>([]);
  const [selectedImporter, setSelectedImporter] = useState("");
  const [selectedImporterKey, setSelectedImporterKey] = useState("");

  const [activeTask, setActiveTask] = useState<FixerTask | null>(null);
  const [progress, setProgress] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [backupPath, setBackupPath] = useState("");
  const [isUpdating, setIsUpdating] = useState(true);
  const [fetchError, setFetchError] = useState(false);
  const versionsRequestId = useRef(0);

  const isRunning = activeTask !== null;

  useEffect(() => {
    void window.api.invoke("tools:4001FixerUpdateReleases").finally(() => {
      setIsUpdating(false);
    });

    void window.api.invoke("tools:4001FixerGetState").then((state) => {
      if (!state) return;

      setActiveTask(state.activeTask);
      setProgress(state.progress || "");
      setErrorMessage(state.errorMessage || "");
    });

    void window.api.invoke("xxmi:getXXMIData").then((data) => {
      const gimiImporters =
        data?.enabledImporters.filter((importer) => importer.key.toUpperCase() === "GIMI") ?? [];
      setImporters(gimiImporters);

      if (gimiImporters.length === 0) return;

      setSelectedImporter(gimiImporters[0].importerFolder);
      setSelectedImporterKey(gimiImporters[0].key);
    });

    const removeListener = window.api.on("tools:4001FixerProgress", (event) => {
      setProgress(event.code);
      setErrorMessage(event.errorMessage || "");

      if (event.task) {
        setActiveTask(event.task);
      }

      if (
        event.code.includes("SUCCESS") ||
        event.code.includes("ALREADY") ||
        event.code.includes("ERR")
      ) {
        setActiveTask(null);
      }
    });

    return () => removeListener();
  }, []);

  useEffect(() => {
    const fetchVersions = async () => {
      const requestId = ++versionsRequestId.current;
      const requestedProvider = provider;

      try {
        const v: string[] = await window.api.invoke(
          "tools:4001FixerGetProviderReleases",
          requestedProvider,
        );
        if (versionsRequestId.current !== requestId) return;

        setVersions(v);
        setVersion(v[0] ?? "");
        setFetchError(false);
      } catch {
        if (versionsRequestId.current !== requestId) return;

        setVersion("");
        setFetchError(true);
      }
    };

    if (!isUpdating) {
      setVersions(null);
      setVersion("");
      setFetchError(false);
      void fetchVersions();
    }
  }, [provider, isUpdating]);

  useEffect(() => {
    if (!selectedImporter) {
      setBackupPath("");
      return;
    }

    let cancelled = false;

    void window.api
      .invoke("tools:4001FixerGetDiversificationState", { importerPath: selectedImporter })
      .then((state) => {
        if (cancelled) return;
        setBackupPath(state.backupPath ?? "");
      });

    return () => {
      cancelled = true;
    };
  }, [selectedImporter]);

  const requireImporter = () => {
    if (selectedImporter) return true;

    setProgress("page.tools.4001_fixer.select_importer_msg");
    setErrorMessage("");
    return false;
  };

  const handleBuild = async () => {
    if (isRunning || !requireImporter()) return;

    setActiveTask("build-dll");
    setProgress("XXMI_INIT");
    setErrorMessage("");
    setBackupPath("");

    try {
      const result = await window.api.invoke("tools:4001FixerBuildDll", {
        provider,
        version,
        importerKey: selectedImporterKey,
        importerPath: selectedImporter,
      });
      if (result?.success === false) {
        setErrorMessage(result && typeof result === "object" ? result.errorMessage || "" : "");
        setActiveTask(null);
        return;
      }
      if (result?.success) {
        setBackupPath("");
      }
    } catch (error) {
      console.error(error);
      setProgress(`Error: ${String(error)}`);
      setErrorMessage(error instanceof Error ? error.message : String(error));
      setActiveTask(null);
    }
  };

  const handleDiversify = async () => {
    if (isRunning || !requireImporter()) return;

    setActiveTask("diversify-dll");
    setProgress("XXMI_OBFUSCATE_INIT");
    setErrorMessage("");
    setBackupPath("");

    try {
      const result = await window.api.invoke("tools:4001FixerDiversifyDllPadding", {
        importerKey: selectedImporterKey,
        importerPath: selectedImporter,
      });
      if (result?.backupPath) {
        setBackupPath(result.backupPath);
      }
      if (result?.success === false) {
        setErrorMessage(result && typeof result === "object" ? result.errorMessage || "" : "");
        setActiveTask(null);
      }
    } catch (error) {
      console.error(error);
      setProgress(`Error: ${String(error)}`);
      setErrorMessage(error instanceof Error ? error.message : String(error));
      setActiveTask(null);
    }
  };

  const handleRestore = async () => {
    if (isRunning || !requireImporter()) return;

    setActiveTask("restore-dll");
    setProgress("XXMI_RESTORE_INIT");
    setErrorMessage("");

    try {
      const result = await window.api.invoke("tools:4001FixerRestoreDiversifiedDll", {
        importerPath: selectedImporter,
      });
      if (result?.success === false) {
        setErrorMessage(result && typeof result === "object" ? result.errorMessage || "" : "");
        setActiveTask(null);
        return;
      }
      if (result?.success) {
        setBackupPath("");
      }
    } catch (error) {
      console.error(error);
      setProgress(`Error: ${String(error)}`);
      setErrorMessage(error instanceof Error ? error.message : String(error));
      setActiveTask(null);
    }
  };

  const progressText =
    progress.startsWith("XXMI_") || progress.startsWith("page.tools.")
      ? t(
          progress.startsWith("page.tools.")
            ? progress
            : `page.tools.4001_fixer.progress.${progress}`,
          progress,
        )
      : progress;

  return (
    <div className="space-y-6 overflow-y-auto p-4">
      <div>
        <h2 className="text-lg font-semibold text-foreground">
          {t("page.tools.4001_fixer.title")}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {t("page.tools.4001_fixer.description")}
        </p>
      </div>

      <div className="space-y-2 rounded-lg border bg-card p-4 transition-shadow duration-200 hover:shadow">
        <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
          {t("page.tools.4001_fixer.target_importer")}
        </label>
        <div className="flex flex-wrap gap-2">
          {importers.length === 0 && (
            <div className="w-full rounded border border-dashed border-border px-3 py-1.5 text-center text-xs text-muted-foreground">
              {t("page.tools.4001_fixer.no_importer")}
            </div>
          )}
          {importers.map((importer) => (
            <button
              key={importer.key}
              onClick={() => {
                setSelectedImporter(importer.importerFolder);
                setSelectedImporterKey(importer.key);
              }}
              className={`rounded border px-3 py-1.5 font-mono text-xs transition-all ${
                selectedImporter === importer.importerFolder
                  ? "cursor-default border-accent bg-accent text-accent-foreground"
                  : "border-border bg-secondary text-secondary-foreground hover:border-accent/50"
              }`}
              disabled={isRunning}
              title={importer.importerFolder}
            >
              {importer.key}
            </button>
          ))}
        </div>
      </div>

      <Tabs defaultValue="build" className="space-y-4">
        <TabsList>
          <TabsTrigger value="build">
            <HammerIcon />
            {t("page.tools.4001_fixer.build_tab")}
          </TabsTrigger>
          <TabsTrigger value="diversify">
            <ShieldCheckIcon />
            {t("page.tools.4001_fixer.obfuscate_tab")}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="build" className="space-y-4">
          <div className="grid grid-cols-1 gap-4 rounded-lg border bg-card p-4 transition-shadow duration-200 hover:shadow md:grid-cols-2">
            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                {t("page.tools.4001_fixer.provider")}
              </label>
              <div className="flex flex-wrap gap-2">
                {["SpectrumQT"].map((v) => (
                  <button
                    key={v}
                    onClick={() => setProvider(v)}
                    className={`rounded border px-3 py-1.5 font-mono text-xs transition-all ${
                      provider === v
                        ? "border-accent bg-accent text-accent-foreground"
                        : "border-border bg-secondary text-secondary-foreground hover:border-accent/50"
                    }`}
                    disabled={isRunning}
                  >
                    {v}
                  </button>
                ))}
              </div>
            </div>

            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                {t("page.tools.4001_fixer.version")}
              </label>
              <div className="flex flex-wrap gap-2">
                {fetchError ? (
                  <div className="flex items-center gap-2 rounded px-3 py-1.5 text-xs text-destructive">
                    <CircleXIcon className="h-3 w-3" /> {t("page.tools.4001_fixer.load_failed")}
                  </div>
                ) : versions === null ? (
                  <div className="flex items-center gap-2 rounded px-3 py-1.5 text-xs text-muted-foreground">
                    <Loader2Icon className="h-3 w-3 animate-spin" />{" "}
                    {t("page.tools.4001_fixer.loading")}
                  </div>
                ) : versions.length === 0 ? (
                  <div className="rounded px-3 py-1.5 text-xs text-muted-foreground">
                    {t("page.tools.4001_fixer.no_versions")}
                  </div>
                ) : (
                  <Select value={version} onValueChange={setVersion}>
                    <SelectTrigger className="w-full max-w-36">
                      <SelectValue placeholder={t("page.tools.4001_fixer.version")} />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      <SelectGroup>
                        {versions.map((v) => (
                          <SelectItem key={v} value={v}>
                            {v}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                {t("page.tools.4001_fixer.version_hint")}
              </p>
            </div>
          </div>

          <Button
            onClick={handleBuild}
            disabled={
              isRunning ||
              !selectedImporter ||
              fetchError ||
              versions === null ||
              versions.length === 0
            }
            variant="outline"
          >
            {activeTask === "build-dll"
              ? t("page.tools.4001_fixer.building")
              : t("page.tools.4001_fixer.start_build")}
          </Button>
        </TabsContent>

        <TabsContent value="diversify" className="space-y-4">
          <div className="rounded-lg border bg-card p-4 transition-shadow duration-200 hover:shadow">
            <p className="text-sm text-muted-foreground">
              {backupPath
                ? t("page.tools.4001_fixer.restore_description")
                : t("page.tools.4001_fixer.obfuscate_description")}
            </p>
          </div>

          <Button
            onClick={backupPath ? handleRestore : handleDiversify}
            disabled={isRunning || !selectedImporter}
            variant="outline"
          >
            {activeTask === "restore-dll" ? (
              <>
                <RotateCcwIcon />
                {t("page.tools.4001_fixer.restoring")}
              </>
            ) : activeTask === "diversify-dll" ? (
              t("page.tools.4001_fixer.obfuscating")
            ) : backupPath ? (
              <>
                <RotateCcwIcon />
                {t("page.tools.4001_fixer.start_restore")}
              </>
            ) : (
              t("page.tools.4001_fixer.start_obfuscate")
            )}
          </Button>
        </TabsContent>
      </Tabs>

      {progress && (
        <div className="rounded-lg border bg-card p-3 transition-shadow duration-200 hover:shadow">
          <div
            className={`flex items-center gap-2 text-sm font-medium animate-in fade-in ${
              progress.includes("ERR") || progress.includes("Error")
                ? "text-destructive"
                : "text-muted-foreground"
            }`}
          >
            {isRunning ? (
              <Loader2Icon className="size-5 shrink-0 animate-spin" />
            ) : progress.includes("ERR") || progress.includes("Error") ? (
              <CircleXIcon className="size-5 shrink-0" />
            ) : progress.includes("SUCCESS") || progress.includes("ALREADY") ? (
              <CircleCheckIcon className="size-5 shrink-0" />
            ) : null}

            <div className="min-w-0">
              <p>{progressText}</p>
              {backupPath && (
                <p className="mt-1 break-all text-xs text-muted-foreground">
                  {t("page.tools.4001_fixer.backup_path", { path: backupPath })}
                </p>
              )}
              {errorMessage && (
                <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap wrap-break-word rounded border bg-muted/40 p-2 font-mono text-xs text-destructive">
                  {errorMessage}
                </pre>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
