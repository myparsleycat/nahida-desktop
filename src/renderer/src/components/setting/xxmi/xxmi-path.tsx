import { Alert, AlertDescription, AlertTitle } from "@renderer/components/ui/alert";
import { Button } from "@renderer/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { Input } from "@renderer/components/ui/input";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { InfoIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIPath({ xxmiData, refetch }: { xxmiData?: XXMIData; refetch: () => void }) {
  const { t } = useTranslation();
  const [showAutoSearchAlert, setShowAutoSearchAlert] = useState(false);
  const [xxmiPath, setXXMIPath] = useState("");

  useEffect(() => {
    setXXMIPath(xxmiData?.xxmiPath || "");
  }, [xxmiData]);

  const saveXXMIPath = async () => {
    try {
      await window.api.invoke("xxmi:saveXXMIPath", xxmiPath);
      toast.success(t("page.setting.xxmi.fn.saveXXMIPath.success"));
      setShowAutoSearchAlert(false);
      refetch();
    } catch (rawErr) {
      const err = (rawErr as Error).message;

      if (err.includes("XXMI Launcher Config.json not found")) {
        toast.warning(t("page.setting.xxmi.fn.saveXXMIPath.configNotFound"));
      }
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("page.setting.xxmi.xxmiPath")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 w-full" aria-describedby={undefined}>
        <div className="flex flex-row w-full space-x-2">
          <Input
            value={xxmiPath}
            onChange={(e) => {
              setXXMIPath(e.target.value);
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

              setXXMIPath(path);
              setShowAutoSearchAlert(true);
            }}
          >
            {t("page.setting.xxmi.autoScan")}
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
      </CardContent>
      <CardFooter className="flex justify-end">
        <Button onClickPromise={saveXXMIPath} disabled={!xxmiPath}>
          {t("g.save")}
        </Button>
      </CardFooter>
    </Card>
  );
}
