import { Button } from "@renderer/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@renderer/components/ui/card";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { Loader2Icon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIDllVersion({ xxmiData }: { xxmiData?: XXMIData }) {
  const { t } = useTranslation();
  const [versions, setVersions] = useState<string[] | null>(null);
  const [version, setVersion] = useState("");
  const [fetchError, setFetchError] = useState(false);

  const hasPath = !!xxmiData?.xxmiPath;

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
    } catch {
      toast.error(t("page.setting.xxmi.fn.installDllVersion.failed"));
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("page.setting.xxmi.dllVersion")}</CardTitle>
        <CardDescription>{t("page.setting.xxmi.dllVersionDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
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
          <Select
            items={versions.map((item) => ({ label: item, value: item }))}
            value={version}
            onValueChange={(value) => {
              if (value === null) return;
              setVersion(value);
            }}
          >
            <SelectTrigger className="w-full max-w-36">
              <SelectValue placeholder={t("page.setting.xxmi.dllVersion")} />
            </SelectTrigger>
            <SelectContent className="h-64">
              <SelectGroup>
                {versions.map((item) => (
                  <SelectItem key={item} value={item}>
                    {item}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        )}
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button
          onClickPromise={applyVersion}
          disabled={!hasPath || !version || fetchError || versions === null}
        >
          {t("g.confirm")}
        </Button>
      </CardFooter>
    </Card>
  );
}
