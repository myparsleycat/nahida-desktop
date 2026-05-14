import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Separator } from "@renderer/components/ui/separator";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { useMemo, useState } from "react";
import { KeyRecorder } from "./key-recorder";
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

interface ActiveKeySetting {
  id: string;
  label: string;
  value: string;
  otherKeys: string[];
  onSave: (newValue: string) => void;
}

export function ModIniList({ mod, expanded, onToggleKeyUpdate }: ModIniListProps) {
  const [activeKeySetting, setActiveKeySetting] = useState<ActiveKeySetting | null>(null);
  const otherKeysById = useMemo(() => {
    const entries = mod.inis.flatMap((ini) =>
      ini.toggleKeys.map((toggleKey, idx) => ({
        id: `${ini.path}:${toggleKey.sectionName}:${idx.toString()}`,
        path: ini.path,
        sectionName: toggleKey.sectionName,
        key: toggleKey.key,
        back: toggleKey.back,
      })),
    );

    return entries.reduce<Record<string, string[]>>((acc, entry) => {
      acc[entry.id] = entries
        .filter((other) => other.sectionName !== entry.sectionName || other.path !== entry.path)
        .flatMap((other) => [other.key, other.back])
        .filter((key): key is string => !!key);
      return acc;
    }, {});
  }, [mod.inis]);

  return (
    <>
      <Dialog
        open={activeKeySetting !== null}
        onOpenChange={(open) => {
          if (!open) {
            setActiveKeySetting(null);
          }
        }}
      >
        <DialogContent onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle className="capitalize">{activeKeySetting?.label ?? ""}</DialogTitle>
            <DialogDescription>Press the key combination to set</DialogDescription>
          </DialogHeader>
          <KeyRecorder
            key={activeKeySetting?.id ?? "empty"}
            defaultValue={activeKeySetting?.value ?? ""}
            otherKeys={activeKeySetting?.otherKeys ?? []}
            onSave={(newValue) => {
              activeKeySetting?.onSave(newValue);
              setActiveKeySetting(null);
            }}
          />
        </DialogContent>
      </Dialog>

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
            "bg-background/10 dark:bg-background/10",
          )}
          // style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
        >
          {mod.inis.map((ini, idx) => (
            <div key={ini.path}>
              <ModIniItem
                ini={ini}
                otherKeysById={otherKeysById}
                onOpenKeySetting={(setting) =>
                  setActiveKeySetting({
                    ...setting,
                    id: `${ini.path}:${setting.id}`,
                    onSave: (newValue) =>
                      onToggleKeyUpdate(
                        mod.path,
                        ini.path,
                        setting.sectionName,
                        setting.label,
                        newValue,
                      ),
                  })
                }
              />
              {idx < mod.inis.length - 1 && <Separator className="my-2" />}
            </div>
          ))}
        </div>
      </ScrollArea>
    </>
  );
}
