import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import { Separator } from "@renderer/components/ui/separator";
import { useSettings } from "@renderer/hooks/use-settings";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/transfer")({
  component: RouteComponent,
});

const settingsConfig = {
  downloadConcurrency: "setting:transfer:getDownloadConcurrency",
  uploadConcurrency: "setting:transfer:getUploadConcurrency",
} as const;

const DOWNLOAD_MIN = 16;
const DOWNLOAD_MAX = 64;
const UPLOAD_MIN = 4;
const UPLOAD_MAX = 16;

function clamp(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) {
    return min;
  }

  return Math.min(max, Math.max(min, Math.trunc(value)));
}

function RouteComponent() {
  const { t } = useTranslation();
  const { settings, update, setSettings, isLoading } = useSettings<{
    downloadConcurrency: number;
    uploadConcurrency: number;
  }>(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleConcurrencyBlur = async (
    key: "downloadConcurrency" | "uploadConcurrency",
    value: number,
  ) => {
    const nextValue =
      key === "downloadConcurrency"
        ? clamp(value, DOWNLOAD_MIN, DOWNLOAD_MAX)
        : clamp(value, UPLOAD_MIN, UPLOAD_MAX);

    setSettings((prev) => ({ ...prev, [key]: nextValue }));

    await update(
      key,
      nextValue,
      key === "downloadConcurrency"
        ? "setting:transfer:setDownloadConcurrency"
        : "setting:transfer:setUploadConcurrency",
    );
  };

  return (
    <div className="space-y-6 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            {t("page.setting.transfer.title")}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.transfer.downloadConcurrency.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.transfer.downloadConcurrency.description")}
              </p>
            </div>
            <Input
              type="number"
              min={DOWNLOAD_MIN}
              max={DOWNLOAD_MAX}
              step={1}
              value={settings.downloadConcurrency}
              onChange={(e) =>
                setSettings((prev) => ({
                  ...prev,
                  downloadConcurrency: Number(e.target.value),
                }))
              }
              onBlur={(e) => handleConcurrencyBlur("downloadConcurrency", Number(e.target.value))}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.currentTarget.blur();
                }
              }}
              className="w-28"
            />
          </div>

          <Separator />

          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.transfer.uploadConcurrency.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.transfer.uploadConcurrency.description")}
              </p>
            </div>
            <Input
              type="number"
              min={UPLOAD_MIN}
              max={UPLOAD_MAX}
              step={1}
              value={settings.uploadConcurrency}
              onChange={(e) =>
                setSettings((prev) => ({
                  ...prev,
                  uploadConcurrency: Number(e.target.value),
                }))
              }
              onBlur={(e) => handleConcurrencyBlur("uploadConcurrency", Number(e.target.value))}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.currentTarget.blur();
                }
              }}
              className="w-28"
            />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
