import { useImporterIcons } from "@renderer/hooks/use-importer-icons";
import { localFileSrc } from "@renderer/lib/local-file";
import { useState } from "react";

import efmiIcon from "@/renderer/assets/xxmi/game_icon/efmi.png";
import gimiIcon from "@/renderer/assets/xxmi/game_icon/gimi.png";
import himiIcon from "@/renderer/assets/xxmi/game_icon/himi.png";
import srmiIcon from "@/renderer/assets/xxmi/game_icon/srmi.png";
import wwmiIcon from "@/renderer/assets/xxmi/game_icon/wwmi.png";
import zzmiIcon from "@/renderer/assets/xxmi/game_icon/zzmi.png";

const bundledIcons: Record<string, string> = {
  GIMI: gimiIcon,
  SRMI: srmiIcon,
  ZZMI: zzmiIcon,
  HIMI: himiIcon,
  WWMI: wwmiIcon,
  EFMI: efmiIcon,
};

export function GameIcon({ gameName, className }: { gameName: string; className?: string }) {
  const { data: icons, dataUpdatedAt } = useImporterIcons();
  const icon = icons?.find((entry) => entry.key === gameName);
  const customPath = icon?.custom && !icon.unavailable ? icon.path : "";
  // A custom icon path is named by its content hash, so reapplying the same image keeps the path. Tying the
  // failure to the fetch that produced it lets a refreshed icon list try the image again.
  const attempt = `${dataUpdatedAt}:${customPath}`;
  const [failedAttempt, setFailedAttempt] = useState("");
  const bundled = bundledIcons[gameName] ?? gimiIcon;

  return (
    <img
      src={customPath && attempt !== failedAttempt ? localFileSrc(customPath) : bundled}
      alt={gameName}
      className={className}
      onError={() => {
        if (customPath) setFailedAttempt(attempt);
      }}
    />
  );
}
