import { Button } from "@renderer/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { CircleCheckIcon, CircleXIcon, Loader2Icon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

interface XXMIImporter {
  key: string;
  importerFolder: string;
}

export default function D3D11Builder() {
  const { t } = useTranslation();

  const [provider, setProvider] = useState("SpectrumQT");
  const [versions, setVersions] = useState<string[] | null>(null);
  const [version, setVersion] = useState("");

  const [importers, setImporters] = useState<XXMIImporter[]>([]);
  const [selectedImporter, setSelectedImporter] = useState<string>("");
  const [selectedImporterKey, setSelectedImporterKey] = useState<string>("");

  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState("");
  const [buildErrorMessage, setBuildErrorMessage] = useState("");
  const [isUpdating, setIsUpdating] = useState(true);
  const [fetchError, setFetchError] = useState(false);
  const versionsRequestId = useRef(0);

  useEffect(() => {
    window.api.invoke("tools:updateReleases").finally(() => {
      setIsUpdating(false);
    });

    window.api.invoke("tools:getBuilderState").then((state) => {
      if (state) {
        setIsRunning(state.isBuilding);
        setProgress(
          state.progress
            ? state.progress.startsWith("XXMI_") || state.progress.startsWith("Error:")
              ? t(`page.tools.d3d11_builder.progress.${state.progress}`, state.progress)
              : state.progress
            : "",
        );
        setBuildErrorMessage(state.errorMessage || "");
      }
    });

    window.api.invoke("xxmi:getXXMIData").then((data) => {
      if (data && data.enabledImporters) {
        setImporters(data.enabledImporters);
        if (data.enabledImporters.length > 0) {
          setSelectedImporter(data.enabledImporters[0].importerFolder);
          setSelectedImporterKey(data.enabledImporters[0].key);
        }
      }
    });

    const removeListener = window.api.on("tools:progress", (code: string) => {
      setProgress(code);
      if (!code.startsWith("XXMI_ERR_")) {
        setBuildErrorMessage("");
      }
      if (code === "XXMI_BUILD_SUCCESS" || code.startsWith("XXMI_ERR_")) {
        setIsRunning(false);
      }
    });

    return () => removeListener();
  }, []);

  useEffect(() => {
    const fetchVersions = async () => {
      const requestId = ++versionsRequestId.current;
      const requestedProvider = provider;

      try {
        const v: string[] = await window.api.invoke("tools:getProviderReleases", requestedProvider);
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
      fetchVersions();
    }
  }, [provider, isUpdating]);

  const handleBuild = async () => {
    if (isRunning) return;
    if (!selectedImporter) {
      setProgress("page.tools.d3d11_builder.select_importer_msg");
      return;
    }

    setIsRunning(true);
    setProgress("XXMI_INIT");
    setBuildErrorMessage("");
    try {
      const result = await window.api.invoke("tools:buildNewD3DDLL", {
        provider,
        version,
        importerKey: selectedImporterKey,
        importerPath: selectedImporter,
      });
      if (result?.success === false) {
        setBuildErrorMessage(result && typeof result === "object" ? result.errorMessage || "" : "");
        setIsRunning(false);
      }
    } catch (e) {
      console.error(e);
      setProgress(`Error: ${e}`);
      setBuildErrorMessage(e instanceof Error ? e.message : String(e));
      setIsRunning(false);
    }
  };

  return (
    <div className="space-y-6 p-4">
      <div>
        <h2 className="text-lg font-semibold text-foreground">
          {t("page.tools.d3d11_builder.title")}
        </h2>
        <p className="text-sm text-muted-foreground mt-1">
          {t("page.tools.d3d11_builder.description")}
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 border bg-card p-4 rounded-lg hover:shadow transition-shadow duration-200">
        <div className="space-y-2">
          <label className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
            {t("page.tools.d3d11_builder.provider")}
          </label>
          <div className="flex flex-wrap gap-2">
            {["SpectrumQT"].map((v) => (
              <button
                key={v}
                onClick={() => setProvider(v)}
                className={`px-3 py-1.5 rounded text-xs font-mono transition-all border ${
                  provider === v
                    ? "bg-accent text-accent-foreground border-accent"
                    : "bg-secondary text-secondary-foreground border-border hover:border-accent/50"
                }`}
                disabled={isRunning}
              >
                {v}
              </button>
            ))}
          </div>
        </div>

        <div className="space-y-2">
          <label className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
            {t("page.tools.d3d11_builder.version")}
          </label>
          <div className="flex flex-wrap gap-2">
            {fetchError ? (
              <div className="px-3 py-1.5 rounded text-xs text-destructive flex items-center gap-2">
                <CircleXIcon className="w-3 h-3" /> {t("page.tools.d3d11_builder.load_failed")}
              </div>
            ) : versions === null ? (
              <div className="px-3 py-1.5 rounded text-xs text-muted-foreground flex items-center gap-2">
                <Loader2Icon className="w-3 h-3 animate-spin" />{" "}
                {t("page.tools.d3d11_builder.loading")}
              </div>
            ) : versions.length === 0 ? (
              <div className="px-3 py-1.5 rounded text-xs text-muted-foreground">
                {t("page.tools.d3d11_builder.no_versions")}
              </div>
            ) : (
              <Select value={version} onValueChange={setVersion}>
                <SelectTrigger className="w-full max-w-36">
                  <SelectValue placeholder={t("page.tools.d3d11_builder.version")} />
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
            {t("page.tools.d3d11_builder.version_hint")}
          </p>
        </div>

        <div className="space-y-2 md:col-span-2 mt-2">
          <label className="text-xs font-medium text-muted-foreground uppercase tracking-widest">
            {t("page.tools.d3d11_builder.target_importer")}
          </label>
          <div className="flex flex-wrap gap-2">
            {importers.length === 0 && (
              <div className="px-3 py-1.5 rounded text-xs text-muted-foreground border border-dashed border-border w-full text-center">
                {t("page.tools.d3d11_builder.no_importer")}
              </div>
            )}
            {importers.map((importer) => (
              <button
                key={importer.key}
                onClick={() => {
                  setSelectedImporter(importer.importerFolder);
                  setSelectedImporterKey(importer.key);
                }}
                className={`px-3 py-1.5 rounded text-xs font-mono transition-all border ${
                  selectedImporter === importer.importerFolder
                    ? "bg-accent text-accent-foreground border-accent cursor-default"
                    : "bg-secondary text-secondary-foreground border-border hover:border-accent/50"
                }`}
                disabled={isRunning}
                title={importer.importerFolder}
              >
                {importer.key}
              </button>
            ))}
          </div>
        </div>
      </div>

      {progress && (
        <div className="p-3 bg-card border rounded-lg hover:shadow transition-shadow duration-200">
          <div
            className={`flex items-center gap-2 text-sm font-medium animate-in fade-in ${progress.includes("ERR") || progress.includes("Error") ? "text-destructive" : "text-muted-foreground"}`}
          >
            {isRunning ? (
              <Loader2Icon className="size-5 shrink-0 animate-spin" />
            ) : progress.includes("ERR") || progress.includes("Error") ? (
              <CircleXIcon className="size-5 shrink-0" />
            ) : progress.includes("SUCCESS") ? (
              <CircleCheckIcon className="size-5 shrink-0" />
            ) : null}

            <div className="min-w-0">
              <p>
                {progress.startsWith("XXMI_") || progress.startsWith("page.tools.")
                  ? t(
                      progress.startsWith("page.tools.")
                        ? progress
                        : `page.tools.d3d11_builder.progress.${progress}`,
                      progress,
                    )
                  : progress}
              </p>
              {buildErrorMessage && (
                <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap wrap-break-word rounded border bg-muted/40 p-2 text-xs font-mono text-destructive">
                  {buildErrorMessage}
                </pre>
              )}
            </div>
          </div>
        </div>
      )}

      <div className="flex items-center gap-4">
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
          {isRunning
            ? t("page.tools.d3d11_builder.building")
            : t("page.tools.d3d11_builder.start_build")}
        </Button>
      </div>
    </div>
  );
}
