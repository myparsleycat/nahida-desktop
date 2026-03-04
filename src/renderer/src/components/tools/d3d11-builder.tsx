// oxlint-disable jsx_a11y/label-has-associated-control
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
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

interface XXMIImporter {
  key: string;
  importerFolder: string;
}

export default function D3D11Builder() {
  const { t } = useTranslation();

  const [provider, setProvider] = useState("SpectrumQT");
  const [versions, setVersions] = useState<string[]>([]);
  const [version, setVersion] = useState("");

  const [importers, setImporters] = useState<XXMIImporter[]>([]);
  const [selectedImporter, setSelectedImporter] = useState<string>("");
  const [selectedImporterKey, setSelectedImporterKey] = useState<string>("");

  const [isRunning, setIsRunning] = useState(false);
  const [progress, setProgress] = useState("");
  const [isUpdating, setIsUpdating] = useState(true);

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
      if (code === "XXMI_BUILD_SUCCESS" || code.startsWith("XXMI_ERR_")) {
        setIsRunning(false);
      }
    });

    return () => removeListener();
  }, []);

  useEffect(() => {
    const fetchVersions = async () => {
      try {
        const v: string[] = await window.api.invoke("tools:getProviderReleases", provider);
        setVersions(v);
        setVersion("master");
      } catch {
        setVersions(["master"]);
        setVersion("master");
      }
    };
    if (!isUpdating) {
      setVersions([]);
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
    try {
      const result = await window.api.invoke("tools:buildNewD3DDLL", {
        provider,
        version,
        importerKey: selectedImporterKey,
        importerPath: selectedImporter,
      });
      if (result === false) {
        setIsRunning(false);
      }
    } catch (e) {
      console.error(e);
      setProgress(`Error: ${e}`);
      setIsRunning(false);
    }
  };

  return (
    <div className="space-y-6">
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
            {["SpectrumQT", "myparsleycat"].map((v) => (
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
            {versions.length === 0 ? (
              <div className="px-3 py-1.5 rounded text-xs text-muted-foreground flex items-center gap-2">
                <Loader2Icon className="w-3 h-3 animate-spin" />{" "}
                {t("page.tools.d3d11_builder.loading")}
              </div>
            ) : (
              <Select value={version} onValueChange={setVersion}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("page.tools.d3d11_builder.version")} />
                </SelectTrigger>
                <SelectContent position="popper" className="max-h-64">
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
              <Loader2Icon className="size-5 animate-spin" />
            ) : progress.includes("ERR") || progress.includes("Error") ? (
              <CircleXIcon className="size-5" />
            ) : progress.includes("SUCCESS") ? (
              <CircleCheckIcon className="size-5" />
            ) : null}

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
          </div>
        </div>
      )}

      <div className="flex items-center gap-4">
        <Button
          onClick={handleBuild}
          disabled={isRunning || !selectedImporter || versions.length === 0}
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
