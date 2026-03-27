import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Separator } from "@renderer/components/ui/separator";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { ModIniItem } from "./mod-ini-item";

interface ModIniListProps {
  mod: ModInfo;
  expanded: boolean;
  onToggleKeyUpdate: (
    modPath: string,
    iniFileName: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

export function ModIniList({ mod, expanded, onToggleKeyUpdate }: ModIniListProps) {
  return (
    <ScrollArea
      className={cn(
        "flex flex-col gap-2 overflow-y-auto transition-all duration-200",
        expanded ? "w-42.5 opacity-100" : "w-0 opacity-0",
      )}
    >
      <div
        className={cn(
          "p-1.5 rounded space-y-2 overflow-hidden transition-all duration-200",
          expanded ? "w-42.5" : "w-0 p-0",
          "backdrop-blur-xl bg-background/10 dark:bg-background/10",
        )}
        style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
      >
        {mod.inis.map((ini, idx) => (
          <div key={idx.toString()}>
            <ModIniItem mod={mod} ini={ini} onToggleKeyUpdate={onToggleKeyUpdate} />
            {idx < mod.inis.length - 1 && <Separator className="my-2" />}
          </div>
        ))}
      </div>
    </ScrollArea>
  );
}
