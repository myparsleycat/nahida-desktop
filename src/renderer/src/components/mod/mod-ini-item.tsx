import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Kbd } from "@renderer/components/ui/kbd";
import { cn } from "@renderer/lib/utils";
import type { ModInfo, ModIni, ToggleKey } from "@renderer/types/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { FileCogIcon, PlusIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { KeyRecorder } from "./key-recorder";

interface ActiveKeySetting {
  id: string;
  label: string;
  value: string;
  otherKeys: string[];
  onSave: (newValue: string) => void;
}

interface KeySettingRequest {
  id: string;
  label: string;
  value: string;
  otherKeys: string[];
}

interface ModToggleKeyItemProps {
  toggleKey: ToggleKey;
  otherKeys: string[];
  onOpenKeySetting: (setting: KeySettingRequest) => void;
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
  activeKeySetting,
  onOpenChange,
}: {
  activeKeySetting: ActiveKeySetting | null;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={activeKeySetting !== null} onOpenChange={onOpenChange}>
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
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

function KeySettingTrigger({
  label,
  value,
  onOpen,
  children,
}: {
  label: string;
  value: string;
  onOpen: () => void;
  children?: ReactNode;
}) {
  const trigger = children ?? (
    <button
      type="button"
      className={cn(
        "flex flex-row items-center w-full transition-colors",
        "border border-white/20 space-x-1 bg-foreground/5 hover:bg-background/10 p-2 rounded-lg",
        "justify-start flex-row size-full p-1",
      )}
      onClick={(e) => {
        e.stopPropagation();
        onOpen();
      }}
    >
      <span className="text-sm">{label}:</span>
      <KeyDisplay keys={value} />
    </button>
  );

  if (children) {
    return (
      <div
        onClick={(e) => {
          e.stopPropagation();
          onOpen();
        }}
      >
        {children}
      </div>
    );
  }

  return trigger;
}

function ModToggleKeyItem({ toggleKey, otherKeys, onOpenKeySetting }: ModToggleKeyItemProps) {
  return (
    <div className="space-y-1 rounded-lg shadow-sm p-1 bg-foreground/5">
      <div className="p-0 h-4.5 grid grid-cols-[1.5rem_1fr]">
        <p className="h-5 w-5 rounded flex items-center justify-center border border-black/15 dark:border-white/15">
          {toggleKey.values.length}
        </p>
        <p className="truncate">{toggleKey.sectionName}</p>
      </div>

      {toggleKey.key ? (
        <KeySettingTrigger
          label="key"
          value={toggleKey.key}
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:key`,
              label: "key",
              value: toggleKey.key ?? "",
              otherKeys,
            })
          }
        />
      ) : (
        <KeySettingTrigger
          label="key"
          value=""
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:key`,
              label: "key",
              value: "",
              otherKeys,
            })
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
        </KeySettingTrigger>
      )}

      {toggleKey.back ? (
        <KeySettingTrigger
          label="back"
          value={toggleKey.back}
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:back`,
              label: "back",
              value: toggleKey.back ?? "",
              otherKeys,
            })
          }
        />
      ) : (
        <KeySettingTrigger
          label="back"
          value=""
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:back`,
              label: "back",
              value: "",
              otherKeys,
            })
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
        </KeySettingTrigger>
      )}
    </div>
  );
}

export function ModIniItem({ mod, ini, onToggleKeyUpdate }: ModIniItemProps) {
  const [activeKeySetting, setActiveKeySetting] = useState<ActiveKeySetting | null>(null);
  const otherKeys = mod.inis.flatMap((i) =>
    i.toggleKeys.map((t) => ({ key: t.key, sectionName: t.sectionName, path: i.path })),
  );

  return (
    <div className="space-y-1 text-[13px]">
      <KeySettingDialog
        activeKeySetting={activeKeySetting}
        onOpenChange={(open) => {
          if (!open) setActiveKeySetting(null);
        }}
      />
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
              toggleKey={toggleKey}
              otherKeys={otherKeys
                .filter((o) => o.sectionName !== toggleKey.sectionName || o.path !== ini.path)
                .map((o) => o.key)
                .filter((k): k is string => !!k)}
              onOpenKeySetting={(setting) =>
                setActiveKeySetting({
                  ...setting,
                  id: `${ini.path}:${setting.id}`,
                  onSave: (newValue) =>
                    onToggleKeyUpdate(
                      mod.path,
                      ini.path,
                      toggleKey.sectionName,
                      setting.label,
                      newValue,
                    ),
                })
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}
