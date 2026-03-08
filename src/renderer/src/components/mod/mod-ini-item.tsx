import { Button } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
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
import { FileCogIcon, PlusIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
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
  onDelete,
  children,
}: {
  label: string;
  value: string;
  otherKeys: string[];
  onSave: (newValue: string) => void;
  onDelete?: () => void;
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const trigger = children ?? (
    <button
      type="button"
      className={cn(
        "flex flex-row items-center w-full transition-colors",
        "border border-white/20 space-x-1 bg-foreground/5 hover:bg-background/10 p-2 rounded-lg",
        "justify-start flex-row size-full p-1",
      )}
    >
      <span className="text-sm">{label}:</span>
      <KeyDisplay keys={value} />
    </button>
  );

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {onDelete ? (
        <ContextMenu>
          <ContextMenuTrigger asChild>
            <DialogTrigger asChild onClick={(e) => e.stopPropagation()}>
              {trigger}
            </DialogTrigger>
          </ContextMenuTrigger>
          <ContextMenuContent onClick={(e) => e.stopPropagation()}>
            <ContextMenuItem
              variant="destructive"
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              Delete {label}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        <DialogTrigger asChild onClick={(e) => e.stopPropagation()}>
          {trigger}
        </DialogTrigger>
      )}
      <DialogContent onClick={(e) => e.stopPropagation()}>
        <DialogHeader>
          <DialogTitle className="capitalize">{label}</DialogTitle>
          <DialogDescription>Press the key combination to set</DialogDescription>
        </DialogHeader>
        <KeyRecorder
          defaultValue={value}
          otherKeys={otherKeys}
          onSave={(newValue) => {
            onSave(newValue);
            setOpen(false);
          }}
        />
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
        <KeySettingDialog
          label="key"
          value={toggleKey.key}
          otherKeys={otherKeys}
          onSave={(newValue) =>
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", newValue)
          }
          onDelete={() => onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", "")}
        />
      ) : (
        <KeySettingDialog
          label="key"
          value=""
          otherKeys={otherKeys}
          onSave={(newValue) =>
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "key", newValue)
          }
        >
          <Button
            variant="outline"
            size="sm"
            className="w-full justify-start text-xs text-muted-foreground"
          >
            <PlusIcon className="size-3 mr-1.5" />
            Add key
          </Button>
        </KeySettingDialog>
      )}

      {toggleKey.back ? (
        <KeySettingDialog
          label="back"
          value={toggleKey.back}
          otherKeys={otherKeys}
          onSave={(newValue) =>
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", newValue)
          }
          onDelete={() => onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", "")}
        />
      ) : (
        <KeySettingDialog
          label="back"
          value=""
          otherKeys={otherKeys}
          onSave={(newValue) =>
            onToggleKeyUpdate(modPath, iniPath, toggleKey.sectionName, "back", newValue)
          }
        >
          <Button
            variant="outline"
            size="sm"
            className={cn(
              "flex flex-row items-center w-full transition-colors dark:text-muted-foreground",
              "border border-white/20 space-x-1 bg-foreground/5 hover:bg-background/10 p-2 rounded-lg",
              "justify-start flex-row size-full p-1",
            )}
          >
            <PlusIcon className="size-3 mr-1.5" />
            Add back
          </Button>
        </KeySettingDialog>
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
