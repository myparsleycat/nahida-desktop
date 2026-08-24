import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Input } from "@renderer/components/ui/input";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { InfoIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIPath({ xxmiData, refetch }: { xxmiData?: XXMIData; refetch: () => void }) {
  const { t } = useTranslation();
  const [showAutoSearchAlert, setShowAutoSearchAlert] = useState(false);
  const [customPath, setCustomPath] = useState<string | null>(null);

  const xxmiPath = customPath ?? xxmiData?.xxmiPath ?? "";

  const saveXXMIPath = async () => {
    try {
      await window.api.invoke("xxmi:saveXXMIPath", xxmiPath);
      toast.success(t("page.setting.xxmi.fn.saveXXMIPath.success"));
      setShowAutoSearchAlert(false);
      setCustomPath(null);
      refetch();
    } catch (rawErr) {
      const err = (rawErr as Error).message;

      if (err.includes("XXMI Launcher Config.json not found")) {
        toast.warning(t("page.setting.xxmi.fn.saveXXMIPath.configNotFound"));
      }
    }
  };

  return (
    <div className="space-y-3">
      <div className="space-y-0.5">
        <span className="text-sm font-medium">{t("page.setting.xxmi.xxmiPath")}</span>
      </div>
      <div className="flex w-full flex-row space-x-2">
        <Input
          value={xxmiPath}
          onChange={(e) => {
            setCustomPath(e.target.value);
          }}
        />
        <Button
          variant="outline"
          onClickPromise={async () => {
            setShowAutoSearchAlert(false);
            const path = await window.api.invoke("xxmi:findXXMIPath");

            if (!path) {
              toast.error(t("page.setting.xxmi.fn.findXXMIPath.xxmiNotFound"));
              return;
            }

            setCustomPath(path);
            setShowAutoSearchAlert(true);
          }}
        >
          {t("page.setting.xxmi.autoScan")}
        </Button>
        <Button
          onClickPromise={saveXXMIPath}
          disabled={!xxmiPath || xxmiPath === (xxmiData?.xxmiPath ?? "")}
        >
          {t("g.save")}
        </Button>
      </div>
      {showAutoSearchAlert && (
        <Alert>
          <InfoIcon />
          <AlertTitle>{t("page.setting.xxmi.fn.findXXMIPath.alert.title")}</AlertTitle>
          <AlertDescription className="text-wrap">
            {t("page.setting.xxmi.fn.findXXMIPath.alert.description")}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}
