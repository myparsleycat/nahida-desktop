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
import { Loader2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIDllVersion({
  xxmiData,
  refetch,
}: {
  xxmiData?: XXMIData;
  refetch: () => void;
}) {
  const { t } = useTranslation();
  const [versions, setVersions] = useState<string[] | null>(null);
  const [version, setVersion] = useState("");
  const [fetchError, setFetchError] = useState(false);

  const hasPath = !!xxmiData?.xxmiPath;
  const isCurrentVersion = (value: string) => isSameDllVersion(value, xxmiData?.dllVersion);
  const currentVersionLabel =
    versions?.find((item) => isCurrentVersion(item)) ?? xxmiData?.dllVersion;

  useEffect(() => {
    let cancelled = false;

    void window.api
      .invoke("xxmi:getLibsReleases")
      .then((releases) => {
        if (cancelled) return;
        setVersions(releases);
        setVersion(releases[0] ?? "");
        setFetchError(false);
      })
      .catch(() => {
        if (cancelled) return;
        setVersions([]);
        setVersion("");
        setFetchError(true);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const applyVersion = async () => {
    try {
      await window.api.invoke("xxmi:installDllVersion", { version });
      toast.success(t("page.setting.xxmi.fn.installDllVersion.success", { version }));
      refetch();
    } catch (error) {
      toast.error(
        toErrorMessage(error).includes("XXMI Launcher")
          ? t("page.setting.xxmi.fn.installDllVersion.launcherCloseFailed")
          : t("page.setting.xxmi.fn.installDllVersion.failed"),
      );
    }
  };

  return (
    <div className="flex items-center justify-between gap-6">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{t("page.setting.xxmi.dllVersion")}</span>
        <p className="text-xs text-muted-foreground">
          {t("page.setting.xxmi.dllVersionDescription")}
        </p>
        {hasPath && (
          <p className="text-xs text-muted-foreground">
            {t("page.setting.xxmi.dllVersionCurrent", {
              version: currentVersionLabel ?? t("page.setting.xxmi.dllVersionUnknown"),
            })}
          </p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {xxmiData && !hasPath ? (
          <p className="text-sm text-muted-foreground">
            {t("page.setting.xxmi.persistNotFoundXXMI")}
          </p>
        ) : fetchError ? (
          <p className="text-sm text-destructive">{t("page.setting.xxmi.dllVersionLoadFailed")}</p>
        ) : versions === null ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2Icon className="size-3.5 animate-spin" />
            {t("page.setting.xxmi.dllVersionLoading")}
          </div>
        ) : versions.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("page.setting.xxmi.dllVersionEmpty")}</p>
        ) : (
          <>
            <Select
              items={versions.map((item) => ({ label: item, value: item }))}
              value={version}
              onValueChange={(value) => {
                if (value === null) return;
                setVersion(value);
              }}
            >
              <SelectTrigger className="w-36">
                <SelectValue placeholder={t("page.setting.xxmi.dllVersion")} />
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
              disabled={
                !hasPath || !version || fetchError || versions === null || isCurrentVersion(version)
              }
            >
              {t("g.confirm")}
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

function isSameDllVersion(selected?: string | null, installed?: string | null) {
  if (!selected || !installed) return false;
  return normalizeDllVersion(selected) === normalizeDllVersion(installed);
}

function normalizeDllVersion(value: string) {
  return value.trim().replace(/^v/i, "");
}
