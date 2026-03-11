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
  uploadCreateManyConcurrency: "setting:transfer:getUploadCreateManyConcurrency",
} as const;

const DOWNLOAD_MIN_MAX = [16, 64];
const UPLOAD_MIN_MAX = [4, 16];
const UPLOAD_CREATE_MANY_MIN_MAX = [1, 4];

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
    uploadCreateManyConcurrency: number;
  }>(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleConcurrencyBlur = async (
    key: "downloadConcurrency" | "uploadConcurrency" | "uploadCreateManyConcurrency",
    value: number,
  ) => {
    const nextValue =
      key === "downloadConcurrency"
        ? clamp(value, DOWNLOAD_MIN_MAX[0], DOWNLOAD_MIN_MAX[1])
        : key === "uploadConcurrency"
          ? clamp(value, UPLOAD_MIN_MAX[0], UPLOAD_MIN_MAX[1])
          : clamp(value, UPLOAD_CREATE_MANY_MIN_MAX[0], UPLOAD_CREATE_MANY_MIN_MAX[1]);

    setSettings((prev) => ({ ...prev, [key]: nextValue }));

    await update(
      key,
      nextValue,
      key === "downloadConcurrency"
        ? "setting:transfer:setDownloadConcurrency"
        : key === "uploadConcurrency"
          ? "setting:transfer:setUploadConcurrency"
          : "setting:transfer:setUploadCreateManyConcurrency",
    );
  };

  return (
    <div className="space-y-6 p-4">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">{t("page.setting.transfer.title")}</CardTitle>
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
              min={DOWNLOAD_MIN_MAX[0]}
              max={DOWNLOAD_MIN_MAX[1]}
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
              min={UPLOAD_MIN_MAX[0]}
              max={UPLOAD_MIN_MAX[1]}
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

          <Separator />

          <div className="flex items-center justify-between gap-6">
            <div className="space-y-0.5">
              <span className="text-sm font-medium">
                {t("page.setting.transfer.uploadCreateManyConcurrency.title")}
              </span>
              <p className="text-xs text-muted-foreground">
                {t("page.setting.transfer.uploadCreateManyConcurrency.description")}
              </p>
            </div>
            <Input
              type="number"
              min={UPLOAD_CREATE_MANY_MIN_MAX[0]}
              max={UPLOAD_CREATE_MANY_MIN_MAX[1]}
              step={1}
              value={settings.uploadCreateManyConcurrency}
              onChange={(e) =>
                setSettings((prev) => ({
                  ...prev,
                  uploadCreateManyConcurrency: Number(e.target.value),
                }))
              }
              onBlur={(e) =>
                handleConcurrencyBlur("uploadCreateManyConcurrency", Number(e.target.value))
              }
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
