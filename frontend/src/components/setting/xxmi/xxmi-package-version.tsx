import { XXMI } from "@bindings/xxmi";
import { Button } from "@renderer/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

type EnabledImporter = NonNullable<XXMIData["enabledImporters"]>[number];

export function XXMIPackageVersion({
  xxmiData,
  refetch,
}: {
  xxmiData?: XXMIData;
  refetch: () => void;
}) {
  const { t } = useTranslation();
  const importers = xxmiData?.enabledImporters ?? [];
  const hasPath = !!xxmiData?.xxmiPath;

  if (!hasPath || importers.length === 0) {
    return null;
  }

  return (
    <div className="space-y-3">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{t("page.setting.xxmi.packageVersion")}</span>
        <p className="text-xs text-muted-foreground">
          {t("page.setting.xxmi.packageVersionDescription")}
        </p>
      </div>
      {importers.map((importer) => (
        <XXMIImporterPackageRow key={importer.key} importer={importer} refetch={refetch} />
      ))}
    </div>
  );
}

function XXMIImporterPackageRow({
  importer,
  refetch,
}: {
  importer: EnabledImporter;
  refetch: () => void;
}) {
  const { t } = useTranslation();
  const [selectedVersion, setSelectedVersion] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ["xxmi:getImporterReleases", importer.key],
    queryFn: () => XXMI.GetImporterReleases(importer.key),
  });
  const versions = query.data;
  const version = selectedVersion ?? versions?.[0] ?? "";
  const isCurrentVersion = (value: string) =>
    isSamePackageVersion(value, importer.installedVersion);
  const currentVersionLabel =
    versions?.find((item) => isCurrentVersion(item)) ?? importer.installedVersion;

  const applyVersion = async () => {
    try {
      await XXMI.InstallImporterPackage({ importer: importer.key, version });
      toast.success(
        t("page.setting.xxmi.fn.installImporterPackage.success", {
          importer: importer.key,
          version,
        }),
      );
      refetch();
    } catch (error) {
      toast.error(
        toErrorMessage(error).includes("XXMI Launcher")
          ? t("page.setting.xxmi.fn.installImporterPackage.launcherCloseFailed")
          : t("page.setting.xxmi.fn.installImporterPackage.failed"),
      );
    }
  };

  return (
    <div className="flex items-center justify-between gap-6">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{importer.key}</span>
        <p className="text-xs text-muted-foreground">
          {t("page.setting.xxmi.packageVersionCurrent", {
            version: currentVersionLabel ?? t("page.setting.xxmi.packageVersionUnknown"),
          })}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {query.isError ? (
          <>
            <p className="text-sm text-destructive">
              {t("page.setting.xxmi.packageVersionLoadFailed")}
            </p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              {t("page.setting.xxmi.packageVersionRetry")}
            </Button>
          </>
        ) : query.isPending ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2Icon className="size-3.5 animate-spin" />
            {t("page.setting.xxmi.packageVersionLoading")}
          </div>
        ) : !versions || versions.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.packageVersionEmpty")}
          </p>
        ) : (
          <>
            <Select
              items={versions.map((item) => ({ label: item, value: item }))}
              value={version}
              onValueChange={(value) => {
                if (value === null) return;
                setSelectedVersion(value);
              }}
            >
              <SelectTrigger className="w-36">
                <SelectValue placeholder={t("page.setting.xxmi.packageVersion")} />
              </SelectTrigger>
              <SelectContent className="h-64">
                <SelectGroup>
                  {versions.map((item) => (
                    <SelectItem key={item} value={item} disabled={isCurrentVersion(item)}>
                      {item}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              onClickPromise={applyVersion}
              disabled={!version || query.isError || query.isPending || isCurrentVersion(version)}
            >
              {t("g.confirm")}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function isSamePackageVersion(selected?: string | null, installed?: string | null) {
  if (!selected || !installed) return false;
  return normalizePackageVersion(selected) === normalizePackageVersion(installed);
}

function normalizePackageVersion(value: string) {
  return value.trim().replace(/^v/i, "");
}
