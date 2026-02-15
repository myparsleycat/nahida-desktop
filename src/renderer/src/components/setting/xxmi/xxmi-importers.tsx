import { GameIcon } from "@renderer/components/game-icon";
import { Card, CardContent, CardHeader, CardTitle } from "@renderer/components/ui/card";
import { cn } from "@renderer/lib/utils";
import type { XXMIData } from "@renderer/routes/setting/xxmi";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function XXMIImporters({ xxmiData }: { xxmiData?: XXMIData }) {
  const { t } = useTranslation();
  const [processingKey, setProcessingKey] = useState<string | null>(null);

  if (!xxmiData?.xxmiConfig) {
    return null;
  }

  const handleStartGame = async (key: string) => {
    if (processingKey !== null) return;

    setProcessingKey(key);
    try {
      await window.api.invoke("xxmi:startGame", key);
    } catch (error) {
      toast.error((error as Error).toString());
    } finally {
      setProcessingKey(null);
    }
  };

  const isAnyProcessing = processingKey !== null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("page.setting.xxmi.activeImporter")}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-row space-x-2 justify-evenly">
          {xxmiData.enabledImporters.map((importer) => {
            const isThisProcessing = processingKey === importer.key;

            return (
              <button
                key={importer.key}
                className={cn(
                  "flex flex-col space-y-1 relative group",
                  isAnyProcessing && !isThisProcessing && "opacity-50 cursor-not-allowed",
                )}
                onClick={() => handleStartGame(importer.key)}
                disabled={isAnyProcessing}
              >
                <div className="relative inline-block">
                  <GameIcon gameName={importer.key} className="size-16" />

                  {isThisProcessing && (
                    <div className="absolute inset-0 bg-black/60 flex items-center justify-center rounded-md transition-opacity">
                      <Loader2Icon className="size-8 animate-spin text-white" />
                    </div>
                  )}
                </div>

                <span className="text-center text-xs">{importer.key}</span>
                <span className="text-center text-xs">{importer.packageInfo.latest_version}</span>
              </button>
            );
          })}
        </div>
      </CardContent>
    </Card>
  );
}
