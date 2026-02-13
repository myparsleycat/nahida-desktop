import efmiIcon from "@/renderer/assets/xxmi/game_icon/efmi.png";
import gimiIcon from "@/renderer/assets/xxmi/game_icon/gimi.png";
import himiIcon from "@/renderer/assets/xxmi/game_icon/himi.png";
import srmiIcon from "@/renderer/assets/xxmi/game_icon/srmi.png";
import wwmiIcon from "@/renderer/assets/xxmi/game_icon/wwmi.png";
import zzmiIcon from "@/renderer/assets/xxmi/game_icon/zzmi.png";

export function GameIcon({ gameName, className }: { gameName: string; className?: string }) {
  switch (gameName) {
    case "GIMI":
      return <img src={gimiIcon} alt={gameName} className={className} />;
    case "SRMI":
      return <img src={srmiIcon} alt={gameName} className={className} />;
    case "ZZMI":
      return <img src={zzmiIcon} alt={gameName} className={className} />;
    case "HIMI":
      return <img src={himiIcon} alt={gameName} className={className} />;
    case "WWMI":
      return <img src={wwmiIcon} alt={gameName} className={className} />;
    case "EFMI":
      return <img src={efmiIcon} alt={gameName} className={className} />;
    default:
      return <img src={gimiIcon} alt={gameName} className={className} />;
  }
}
