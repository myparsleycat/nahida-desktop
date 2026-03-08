import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { Kbd } from "@renderer/components/ui/kbd";
import { cn } from "@renderer/lib/utils";
import type { ModInfo, ModIni, ToggleKey } from "@renderer/types/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { FileCogIcon, PlusIcon, TrashIcon } from "lucide-react";
import { KeyRecorder } from "./key-recorder";

interface ModToggleKeyItemProps {
  modPath: string;
  iniPath: string;
  toggleKey: ToggleKey;
  otherKeys: string[];
  onToggleKeyUpdate: (
    modPath: string,
    iniPath: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

function KeyDisplay({ keys }: { keys: string }) {
  return (
    <div className="flex flex-wrap items-center gap-1">
      {keys
        .split(" ")
        .map((k) => formatKeyLabel(k))
        .filter((k): k is string => k !== null)
        .map((label, idx) => (
          <Kbd key={idx.toString()} className="text-xs">
            {label}
          </Kbd>
        ))}
    </div>
  );
}

function KeySettingDialog({
  label,
  value,
  otherKeys,
  onSave,
}: {
  label: string;
  value: string;
  otherKeys: string[];
  onSave: (newValue: string) => void;
}) {
  return (
    <Dialog>
      <DialogTrigger
        className={cn(
          "flex flex-row items-center w-full transition-colors",
          "border border-white/20 space-x-1 bg-foreground/5 hover:bg-background/10 p-2 rounded-lg",
          "justify-start flex-row size-full p-1",
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <span className="text-sm">{label}:</span>
        <KeyDisplay keys={value} />
      </DialogTrigger>
      <DialogContent onClick={(e) => e.stopPropagation()}>
        <DialogHeader>
          <DialogTitle className="capitalize">{label}</DialogTitle>
          <DialogDescription>Press the key combination to set</DialogDescription>
        </DialogHeader>
        <KeyRecorder defaultValue={value} otherKeys={otherKeys} onSave={onSave} />
      </DialogContent>
    </Dialog>
  );
}

function ModToggleKeyItem({
  modPath,
  iniPath,
  toggleKey,
  otherKeys,
  onToggleKeyUpdate,
}: ModToggleKeyItemProps) {
  return (
    <div className="space-y-1 rounded-lg shadow-sm p-1 bg-foreground/5">
      <div className="p-0 h-4.5 grid grid-cols-[1.5rem_1fr]">
        <p className="h-5 w-5 rounded flex items-center justify-center border border-black/15 dark:border-white/15">
          {toggleKey.values.length}
        </p>
        <p className="truncate">{toggleKey.sectionName}</p>
      </div>

      {toggleKey.key ? (
        <div className="flex items-center gap-1">
          <KeySettingDialog
            label="key"
            value={toggleKey.key}
            otherKeys={otherKeys}
            onSave={(newValue) =>
              onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", newValue)
            }
          />
          <Button
            variant="ghost"
            size="icon"
            className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation();
              onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", "");
            }}
          >
            <TrashIcon className="size-4" />
          </Button>
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="w-full justify-start text-xs h-8 text-muted-foreground"
          onClick={(e) => {
            e.stopPropagation();
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", "VK_PRIOR");
          }}
        >
          <PlusIcon className="size-3 mr-1.5" />
          Add key
        </Button>
      )}

      {toggleKey.back ? (
        <div className="flex items-center gap-1">
          <KeySettingDialog
            label="back"
            value={toggleKey.back}
            otherKeys={otherKeys}
            onSave={(newValue) =>
              onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", newValue)
            }
          />
          <Button
            variant="ghost"
            size="icon"
            className="size-7 shrink-0 text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation();
              onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", "");
            }}
          >
            <TrashIcon className="size-4" />
          </Button>
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="w-full justify-start text-xs h-8 text-muted-foreground"
          onClick={(e) => {
            e.stopPropagation();
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", "VK_NEXT");
          }}
        >
          <PlusIcon className="size-3 mr-1.5" />
          Add back
        </Button>
      )}
    </div>
  );
}

interface ModIniItemProps {
  mod: ModInfo;
  ini: ModIni;
  onToggleKeyUpdate: (
    modPath: string,
    iniFileName: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

export function ModIniItem({ mod, ini, onToggleKeyUpdate }: ModIniItemProps) {
  const otherKeys = mod.inis.flatMap((i) =>
    i.toggleKeys.map((t) => ({ key: t.key, sectionName: t.sectionName, path: i.path })),
  );

  return (
    <div className="space-y-1 text-[13px]">
      <div className="flex items-center justify-between gap-1">
        <p className="truncate opacity-80 whitespace-normal" title={ini.name}>
          {ini.name}
        </p>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            window.api.invoke("util:openPath", ini.path);
          }}
        >
          <FileCogIcon />
        </Button>
      </div>

      {ini.toggleKeys.length > 0 && (
        <div className="space-y-2.5">
          {ini.toggleKeys.map((toggleKey, idx) => (
            <ModToggleKeyItem
              key={idx.toString()}
              modPath={mod.path}
              iniPath={ini.path}
              toggleKey={toggleKey}
              otherKeys={otherKeys
                .filter((o) => o.sectionName !== toggleKey.sectionName || o.path !== ini.path)
                .map((o) => o.key)
                .filter((k): k is string => !!k)}
              onToggleKeyUpdate={onToggleKeyUpdate}
            />
          ))}
        </div>
      )}
    </div>
  );
}
