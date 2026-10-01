import { Input } from "@renderer/components/ui/input";
import {
  Section,
  SectionContent,
  SectionHeader,
  SectionRow,
  SectionTitle,
} from "@renderer/components/ui/section";
import { Switch } from "@renderer/components/ui/switch";
import { useSettings } from "@renderer/hooks/use-settings";
import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/setting/transfer")({
  component: RouteComponent,
});

const settingsConfig = {
  downloadConcurrency: "transfer.downloadConcurrency",
  downloadBandwidthLimitMibps: "transfer.downloadBandwidthLimitMibps",
  uploadConcurrency: "transfer.uploadConcurrency",
  moveTransferPageWhenStartTransfer: "general.moveTransferPageWhenStartTransfer",
  powerSaveBlockInTransfer: "general.powerSaveBlockInTransfer",
} as const;

const DOWNLOAD_MIN_MAX = [16, 64];
const DOWNLOAD_BANDWIDTH_MIN_MAX = [0, 1024];
const UPLOAD_MIN_MAX = [4, 16];

function clamp(value: number, min: number, max: number) {
  if (!Number.isFinite(value)) {
    return min;
  }

  return Math.min(max, Math.max(min, Math.trunc(value)));
}

function RouteComponent() {
  const { t } = useTranslation();
  const { settings, update, setSettings, isLoading } = useSettings(settingsConfig);

  if (isLoading) {
    return null;
  }

  const handleNumberBlur = async (
    key: "downloadConcurrency" | "downloadBandwidthLimitMibps" | "uploadConcurrency",
    value: number,
  ) => {
    const nextValue =
      key === "downloadConcurrency"
        ? clamp(value, DOWNLOAD_MIN_MAX[0], DOWNLOAD_MIN_MAX[1])
        : key === "downloadBandwidthLimitMibps"
          ? clamp(value, DOWNLOAD_BANDWIDTH_MIN_MAX[0], DOWNLOAD_BANDWIDTH_MIN_MAX[1])
          : clamp(value, UPLOAD_MIN_MAX[0], UPLOAD_MIN_MAX[1]);

    setSettings((prev) => ({ ...prev, [key]: nextValue }));

    await update(key, nextValue);
  };

  return (
    <div className="space-y-6 p-4">
      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.transfer.concurrency.title")}</SectionTitle>
        </SectionHeader>
        <SectionContent>
          <SectionRow
            title={t("page.setting.transfer.downloadConcurrency.title")}
            description={t("page.setting.transfer.downloadConcurrency.description")}
          >
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
              onBlur={(e) => handleNumberBlur("downloadConcurrency", Number(e.target.value))}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.currentTarget.blur();
                }
              }}
              className="w-28"
            />
          </SectionRow>
          <SectionRow
            title={t("page.setting.transfer.uploadConcurrency.title")}
            description={t("page.setting.transfer.uploadConcurrency.description")}
          >
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
              onBlur={(e) => handleNumberBlur("uploadConcurrency", Number(e.target.value))}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.currentTarget.blur();
                }
              }}
              className="w-28"
            />
          </SectionRow>
        </SectionContent>
      </Section>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.transfer.bandwidth.title")}</SectionTitle>
        </SectionHeader>
        <SectionContent>
          <SectionRow
            title={t("page.setting.transfer.downloadBandwidthLimitMibps.title")}
            description={t("page.setting.transfer.downloadBandwidthLimitMibps.description")}
          >
            <Input
              type="number"
              min={DOWNLOAD_BANDWIDTH_MIN_MAX[0]}
              max={DOWNLOAD_BANDWIDTH_MIN_MAX[1]}
              step={1}
              value={settings.downloadBandwidthLimitMibps}
              onChange={(e) =>
                setSettings((prev) => ({
                  ...prev,
                  downloadBandwidthLimitMibps: Number(e.target.value),
                }))
              }
              onBlur={(e) =>
                handleNumberBlur("downloadBandwidthLimitMibps", Number(e.target.value))
              }
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.currentTarget.blur();
                }
              }}
              className="w-28"
            />
          </SectionRow>
        </SectionContent>
      </Section>

      <Section>
        <SectionHeader>
          <SectionTitle>{t("page.setting.transfer.other.title")}</SectionTitle>
        </SectionHeader>
        <SectionContent>
          <SectionRow
            title={t("page.setting.gen.other.moveTransferPageWhenStartTransfer")}
            description={t("page.setting.gen.other.moveTransferPageWhenStartTransferDescription")}
          >
            <Switch
              checked={settings.moveTransferPageWhenStartTransfer}
              onCheckedChange={(val) => update("moveTransferPageWhenStartTransfer", val)}
            />
          </SectionRow>
          <SectionRow
            title={t("page.setting.gen.other.powerSaveBlockInTransfer")}
            description={t("page.setting.gen.other.powerSaveBlockInTransferDescription")}
          >
            <Switch
              checked={settings.powerSaveBlockInTransfer}
              onCheckedChange={(val) => update("powerSaveBlockInTransfer", val)}
            />
          </SectionRow>
        </SectionContent>
      </Section>
    </div>
  );
}
