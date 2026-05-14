import { Button } from "@renderer/components/ui/button";
import { Kbd } from "@renderer/components/ui/kbd";
import { cn } from "@renderer/lib/utils";
import type { ModIni, ToggleKey } from "@renderer/types/mod";
import { formatKeyLabel } from "@shared/key-formatter";
import { FileCogIcon, PlusIcon } from "lucide-react";
import { type ReactNode, memo, useMemo } from "react";

interface KeySettingRequest {
  id: string;
  label: string;
  sectionName: string;
  value: string;
  otherKeys: string[];
}

interface ModToggleKeyItemProps {
  toggleKey: ToggleKey;
  otherKeys: string[];
  onOpenKeySetting: (setting: KeySettingRequest) => void;
}

interface ModIniItemProps {
  ini: ModIni;
  otherKeysById: Record<string, string[]>;
  onOpenKeySetting: (setting: KeySettingRequest) => void;
}

function KeyDisplay({ keys }: { keys: string }) {
  const labels = useMemo(
    () =>
      keys
        .split(" ")
        .map((k) => formatKeyLabel(k))
        .filter((k): k is string => k !== null),
    [keys],
  );

  return (
    <div className="flex flex-wrap items-center gap-1">
      {labels.map((label, idx) => (
        <Kbd key={`${keys}:${idx.toString()}`} className="text-xs">
          {label}
        </Kbd>
      ))}
    </div>
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

const ModToggleKeyItem = memo(function ModToggleKeyItem({
  toggleKey,
  otherKeys,
  onOpenKeySetting,
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
        <KeySettingTrigger
          label="key"
          value={toggleKey.key}
          onOpen={() =>
            onOpenKeySetting({
              id: `${toggleKey.sectionName}:key`,
              label: "key",
              sectionName: toggleKey.sectionName,
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
              sectionName: toggleKey.sectionName,
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
              sectionName: toggleKey.sectionName,
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
              sectionName: toggleKey.sectionName,
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
});

export const ModIniItem = memo(function ModIniItem({
  ini,
  otherKeysById,
  onOpenKeySetting,
}: ModIniItemProps) {
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
              key={`${ini.path}:${toggleKey.sectionName}:${idx.toString()}`}
              toggleKey={toggleKey}
              otherKeys={
                otherKeysById[`${ini.path}:${toggleKey.sectionName}:${idx.toString()}`] ?? []
              }
              onOpenKeySetting={onOpenKeySetting}
            />
          ))}
        </div>
      )}
    </div>
  );
});
